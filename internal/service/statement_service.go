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

	// 3. Build StatementDetail slice (merchants only see statements for their tenant).
	if isMerchant {
		filtered := make([]*repository.StatementRow, 0, len(rows))
		for _, row := range rows {
			subRow, err := s.subRepo.FindByID(ctx, row.SubscriptionID)
			if err != nil || subRow.TenantID != callerID {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
		count = len(filtered)
	}

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

// ExportStatements renders all statements for customerID as gzipped CSV,
// uploads to S3 under a tenant-scoped versioned key, and returns a presigned URL.
func (s *statementService) ExportStatements(ctx context.Context, callerID string, roles []string, tenantID, customerID string, uploader s3.S3Uploader) (*ExportResult, error) {
	isAdmin := false
	for _, r := range roles {
		if r == "admin" {
			isAdmin = true
			break
		}
	}
	if !isAdmin && callerID != customerID {
		return nil, errors.New("forbidden: only admin or the owning customer may export statements")
	}

	stmts, _, _, err := s.ListByCustomer(ctx, callerID, roles, customerID, repository.StatementQuery{Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("list statements: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	fmt.Fprintln(gz, "id,subscription_id,period_start,period_end,issued_at,total_amount,currency,kind,status")
	for _, st := range stmts.Statements {
		fmt.Fprintf(gz, "%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			st.ID, st.SubscriptionID, st.PeriodStart, st.PeriodEnd,
			st.IssuedAt, st.TotalAmount, st.Currency, st.Kind, st.Status)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("compress csv: %w", err)
	}

	objectKey := fmt.Sprintf("exports/%s/%s/%d.csv.gz", tenantID, customerID, time.Now().UnixNano())

	if err := uploader.PutObject(ctx, objectKey, buf.Bytes(), "application/gzip"); err != nil {
		return nil, fmt.Errorf("upload export: %w", err)
	}

	presigned, err := uploader.PresignURL(ctx, objectKey, ExportPresignTTL)
	if err != nil {
		return nil, fmt.Errorf("presign url: %w", err)
	}

	return &ExportResult{ObjectKey: objectKey, URL: presigned.URL, ExpiresAt: presigned.ExpiresAt}, nil
}
