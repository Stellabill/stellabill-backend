package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/storage/s3"
	"stellarbill-backend/internal/timeutil"
)

// ExportPresignTTL is the default presigned-URL lifetime.
const ExportPresignTTL = 15 * time.Minute

// ExportResult is returned by ExportStatements.
type ExportResult struct {
	// ObjectKey is the versioned S3 key used for the upload.
	// Format: exports/{tenantID}/{customerID}/{uuid}.csv.gz
	ObjectKey string
	// URL is the presigned GET URL valid for ExportPresignTTL.
	URL string
	// ExpiresAt is the UTC timestamp when the presigned URL expires.
	ExpiresAt time.Time
}

// StatementService defines the business logic interface for billing statements.
type StatementService interface {
	GetDetail(ctx context.Context, callerID string, roles []string, statementID string) (*StatementDetail, []string, error)
	ListByCustomer(ctx context.Context, callerID string, roles []string, customerID string, q repository.StatementQuery) (*ListStatementsDetail, int, []string, error)
	// ExportStatements renders all statements for customerID as gzipped CSV,
	// uploads to S3 under a tenant-scoped versioned key, and returns a 15-min
	// presigned URL. Only callers with role "admin" or a merchant whose tenant
	// owns the customer may invoke this; subscribers may not.
	ExportStatements(ctx context.Context, callerID string, roles []string, tenantID, customerID string, uploader s3.S3Uploader) (*ExportResult, error)
}

// statementService is the concrete implementation of StatementService.
type statementService struct {
	subRepo  repository.SubscriptionRepository
	stmtRepo repository.StatementRepository
	objStore cache.ObjectStore // nil-safe: rehydration is optional
}

// NewStatementService constructs a StatementService with the given repositories.
func NewStatementService(subRepo repository.SubscriptionRepository, stmtRepo repository.StatementRepository) StatementService {
	return &statementService{subRepo: subRepo, stmtRepo: stmtRepo}
}

// NewStatementServiceWithArchive constructs a StatementService with archival support.
func NewStatementServiceWithArchive(subRepo repository.SubscriptionRepository, stmtRepo repository.StatementRepository, objStore cache.ObjectStore) StatementService {
	return &statementService{subRepo: subRepo, stmtRepo: stmtRepo, objStore: objStore}
}

// GetDetail retrieves a full StatementDetail for the given statementID.
// It enforces strict RBAC:
// - Admin: always allowed
// - Merchant: allowed if the statement belongs to their tenant (checked via subscription)
// - Subscriber: allowed if they own the statement (callerID == row.CustomerID)
//
// If the statement is archived and object storage is configured, it transparently
// rehydrates the full data from cold storage before returning (with a warning about
// latency). If rehydration fails or is unavailable, it returns the archived stub.
func (s *statementService) GetDetail(ctx context.Context, callerID string, roles []string, statementID string) (*StatementDetail, []string, error) {
	var warnings []string

	// 1. Fetch statement row.
	row, err := s.stmtRepo.FindByID(ctx, statementID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	// 2. Soft-delete check.
	if row.DeletedAt != nil {
		return nil, nil, ErrDeleted
	}

	// 3. RBAC/Ownership check.
	isAdmin := false
	isMerchant := false
	for _, role := range roles {
		if role == "admin" {
			isAdmin = true
			break
		}
		if role == "merchant" {
			isMerchant = true
		}
	}

	isAuthorized := false
	if isAdmin {
		isAuthorized = true
	} else if isMerchant {
		// Verify the statement belongs to this merchant (callerID = tenantID)
		sub, err := s.subRepo.FindByID(ctx, row.SubscriptionID)
		if err == nil && sub.TenantID == callerID {
			isAuthorized = true
		}
	} else if callerID == row.CustomerID {
		isAuthorized = true
	}

	if !isAuthorized {
		return nil, nil, ErrForbidden
	}

	// 4. Check if archived and rehydrate if needed
	if row.ArchivedAt != nil && s.objStore != nil && row.ArchiveKey != "" {
		// Rehydrate from cold storage
		rehydratedRow, err := s.rehydrateFromArchive(ctx, row)
		if err == nil {
			row = rehydratedRow
			warnings = append(warnings, "statement rehydrated from cold storage; latency may be higher than active statements")
		} else {
			// Warn but don't fail - return stub with warning
			warnings = append(warnings, "failed to rehydrate from cold storage: "+err.Error())
		}
	}

	// 5. Build StatementDetail.
	periodStart := normalizeRFC3339OrKeep(row.PeriodStart)
	periodEnd := normalizeRFC3339OrKeep(row.PeriodEnd)
	issuedAt := normalizeRFC3339OrKeep(row.IssuedAt)

	detail := &StatementDetail{
		ID:             row.ID,
		SubscriptionID: row.SubscriptionID,
		Customer:       row.CustomerID,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		IssuedAt:       issuedAt,
		TotalAmount:    row.TotalAmount,
		Currency:       row.Currency,
		Kind:           row.Kind,
		Status:         row.Status,
	}

	return detail, warnings, nil
}

// ListByCustomer retrieves a list of StatementDetails for the given customerID.
// Strict RBAC:
// - Admin: always allowed
// - Merchant: allowed if the customer belongs to their tenant (checked via their subscriptions)
// - Subscriber: allowed if callerID == customerID
func (s *statementService) ListByCustomer(ctx context.Context, callerID string, roles []string, customerID string, q repository.StatementQuery) (*ListStatementsDetail, int, []string, error) {
	var warnings []string

	// 1. RBAC/Ownership check.
	isAdmin := false
	isMerchant := false
	for _, role := range roles {
		if role == "admin" {
			isAdmin = true
			break
		}
		if role == "merchant" {
			isMerchant = true
		}
	}

	isAuthorized := false
	if isAdmin {
		isAuthorized = true
	} else if isMerchant {
		// In a real app, we'd have a merchant_customers relationship.
		// For this implementation, we'll allow merchants to list if they provide a valid merchant-owned subscription filter,
		// or if we have another way to verify. For now, we'll assume they are authorized if they are a merchant
		// BUT we should filter by tenant if possible.
		// Since ListByCustomerID doesn't take tenantID, we might need to add it or trust the caller if it's a merchant.
		// TODO: Hardening: Filter by tenant if merchant.
		isAuthorized = true
	} else if callerID == customerID {
		isAuthorized = true
	}

	if !isAuthorized {
		return nil, 0, nil, ErrForbidden
	}

	// 2. Fetch statement rows for customer with filters and pagination.
	rows, count, err := s.stmtRepo.ListByCustomerID(ctx, customerID, q)
	if err != nil {
		return nil, 0, nil, err
	}

	if isMerchant && !isAdmin {
		var filteredRows []*repository.StatementRow
		for _, row := range rows {
			sub, err := s.subRepo.FindByID(ctx, row.SubscriptionID)
			if err == nil && sub.TenantID == callerID {
				filteredRows = append(filteredRows, row)
			}
		}
		rows = filteredRows
		count = len(rows)
	}

	// 3. Build StatementDetail slice.
	result := &ListStatementsDetail{
		Statements: make([]*StatementDetail, 0, len(rows)),
	}
	for _, row := range rows {
		if isMerchant {
			sub, err := s.subRepo.FindByID(ctx, row.SubscriptionID)
			if err != nil || sub.TenantID != callerID {
				continue
			}
		}

		periodStart := normalizeRFC3339OrKeep(row.PeriodStart)
		periodEnd := normalizeRFC3339OrKeep(row.PeriodEnd)
		issuedAt := normalizeRFC3339OrKeep(row.IssuedAt)

		result.Statements = append(result.Statements, &StatementDetail{
			ID:             row.ID,
			SubscriptionID: row.SubscriptionID,
			Customer:       row.CustomerID,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
			IssuedAt:       issuedAt,
			TotalAmount:    row.TotalAmount,
			Currency:       row.Currency,
			Kind:           row.Kind,
			Status:         row.Status,
		})
	}

	// Update count to reflect filtered result size
	if isMerchant {
		count = len(result.Statements)
	}

	return result, count, warnings, nil
}

func normalizeRFC3339OrKeep(raw string) string {
	normalized, err := timeutil.NormalizeRFC3339StringToUTC(raw)
	if err != nil {
		return raw
	}
	return normalized
}

// rehydrateFromArchive retrieves a statement from cold storage and returns a hydrated StatementRow.
// It includes both the stub metadata (ID, subscription, customer) and the archived payload data.
func (s *statementService) rehydrateFromArchive(ctx context.Context, stub *repository.StatementRow) (*repository.StatementRow, error) {
	if s.objStore == nil || stub.ArchiveKey == "" {
		return stub, errors.New("object store not configured or no archive key")
	}

	// Retrieve JSON from object storage
	data, err := s.objStore.Get(ctx, stub.ArchiveKey)
	if err != nil {
		return nil, errors.New("failed to retrieve archived statement from cold storage: " + err.Error())
	}

	// Unmarshal into payload
	var payload cache.StatementArchivePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("failed to parse archived statement: " + err.Error())
	}

	// Reconstruct StatementRow with hydrated data
	hydrated := &repository.StatementRow{
		ID:             payload.ID,
		SubscriptionID: payload.SubscriptionID,
		CustomerID:     payload.CustomerID,
		PeriodStart:    payload.PeriodStart,
		PeriodEnd:      payload.PeriodEnd,
		IssuedAt:       payload.IssuedAt,
		TotalAmount:    payload.TotalAmount,
		Currency:       payload.Currency,
		Kind:           payload.Kind,
		Status:         payload.Status,
		ArchivedAt:     stub.ArchivedAt,
		ArchiveKey:     stub.ArchiveKey,
		DeletedAt:      stub.DeletedAt,
	}

	// Optionally update the database row with rehydrated data for future cache hits
	// (failures are ignored; this is a best-effort optimization)
	_ = s.stmtRepo.UpdateArchivedData(ctx, payload.ID, hydrated)

	return hydrated, nil
}
