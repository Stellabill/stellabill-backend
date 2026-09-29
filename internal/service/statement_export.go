package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/storage/s3"
)

const (
	exportPresignTTL15m = 15 * time.Minute
	statementPageSize   = 1000
)

// ExportResult is the outcome of a statement export request.
type ExportResult struct {
	ObjectKey string
	URL       string
	ExpiresAt time.Time
}

// ExportStatements enforces RBAC (admin always; merchant only for their own
// tenant), verifies the target customer belongs to the tenant, exports the
// customer's statements as a gzipped CSV to object storage, and returns a
// 15-minute presigned GET URL.
func (s *statementService) ExportStatements(ctx context.Context, callerID string, roles []string, tenantID, customerID string, uploader s3.S3Uploader) (*ExportResult, error) {
	if s == nil {
		return nil, fmt.Errorf("statement service unavailable")
	}
	if uploader == nil {
		return nil, fmt.Errorf("uploader unavailable")
	}

	isAdmin := false
	isMerchant := false
	for _, role := range roles {
		switch role {
		case "admin":
			isAdmin = true
		case "merchant":
			isMerchant = true
		}
	}

	if !isAdmin && !isMerchant {
		return nil, ErrForbidden
	}
	if isMerchant && callerID != tenantID {
		return nil, ErrForbidden
	}

	subs, err := s.subRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("export: list tenant subscriptions: %w", err)
	}

	subIDs := make(map[string]struct{}, len(subs))
	ownsCustomer := false
	for _, sub := range subs {
		subIDs[sub.ID] = struct{}{}
		if sub.CustomerID == customerID {
			ownsCustomer = true
		}
	}
	if !ownsCustomer {
		return nil, ErrForbidden
	}

	rows, _, err := s.stmtRepo.ListByCustomerID(ctx, customerID, repository.StatementQuery{Limit: statementPageSize})
	if err != nil {
		return nil, fmt.Errorf("export: list statements: %w", err)
	}

	filtered := rows[:0]
	for _, r := range rows {
		if _, ok := subIDs[r.SubscriptionID]; ok {
			filtered = append(filtered, r)
		}
	}

	data, err := buildStatementsCSVGZ(filtered)
	if err != nil {
		return nil, fmt.Errorf("export: build csv: %w", err)
	}

	objectKey := fmt.Sprintf(
		"exports/statements/%s/%s/%s.csv.gz",
		tenantID,
		customerID,
		time.Now().UTC().Format("20060102-150405"),
	)

	if err := uploader.PutObject(ctx, objectKey, data, "application/gzip"); err != nil {
		return nil, fmt.Errorf("export: upload: %w", err)
	}

	presigned, err := uploader.PresignURL(ctx, objectKey, exportPresignTTL15m)
	if err != nil {
		return nil, fmt.Errorf("export: presign: %w", err)
	}

	return &ExportResult{
		ObjectKey: objectKey,
		URL:       presigned.URL,
		ExpiresAt: presigned.ExpiresAt,
	}, nil
}

// buildStatementsCSVGZ renders statement rows as a gzipped CSV document.
func buildStatementsCSVGZ(rows []*repository.StatementRow) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	cw := csv.NewWriter(gz)

	if err := cw.Write([]string{
		"id", "tenant_id", "subscription_id", "customer_id",
		"period_start", "period_end", "issued_at",
		"total_amount", "currency", "kind", "status",
	}); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		if err := cw.Write([]string{
			r.ID, r.TenantID, r.SubscriptionID, r.CustomerID,
			r.PeriodStart, r.PeriodEnd, r.IssuedAt,
			r.TotalAmount, r.Currency, r.Kind, r.Status,
		}); err != nil {
			return nil, err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
