package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/storage/s3"

	"github.com/google/uuid"
)

const (
	ExportPresignTTL24h = 24 * time.Hour
	statementsPageSize  = 1000
)

type TenantExportResult struct {
	ObjectKey  string    `json:"object_key"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
	SHA256Hash string    `json:"sha256_hash"`
}

type TenantExportService interface {
	ExportTenantData(ctx context.Context, callerID string, roles []string, tenantID string, uploader s3.S3Uploader) (*TenantExportResult, error)
}

type tenantExportService struct {
	planRepo repository.PlanRepository
	subRepo  repository.SubscriptionRepository
	stmtRepo repository.StatementRepository
}

func NewTenantExportService(
	planRepo repository.PlanRepository,
	subRepo repository.SubscriptionRepository,
	stmtRepo repository.StatementRepository,
) TenantExportService {
	return &tenantExportService{
		planRepo: planRepo,
		subRepo:  subRepo,
		stmtRepo: stmtRepo,
	}
}

func (s *tenantExportService) ExportTenantData(
	ctx context.Context,
	callerID string,
	roles []string,
	tenantID string,
	uploader s3.S3Uploader,
) (*TenantExportResult, error) {
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

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("export cancelled before data fetch: %w", err)
	}

	plans, err := s.planRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("export cancelled after plans: %w", err)
	}

	subs, err := s.subRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	customers := make(map[string]struct{})
	for _, sub := range subs {
		if sub.CustomerID != "" {
			customers[sub.CustomerID] = struct{}{}
		}
	}

	var allStatements []*repository.StatementRow
	for customerID := range customers {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("export cancelled during statement fetch: %w", err)
		}
		q := repository.StatementQuery{
			Limit:    statementsPageSize,
			Page:     1,
			PageSize: statementsPageSize,
		}
		statements, _, err := s.stmtRepo.ListByCustomerID(ctx, customerID, q)
		if err != nil {
			return nil, fmt.Errorf("list statements for customer %s: %w", customerID, err)
		}
		allStatements = append(allStatements, statements...)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("export cancelled before zip build: %w", err)
	}

	zipData, err := buildExportZIP(ctx, plans, subs, allStatements)
	if err != nil {
		return nil, fmt.Errorf("build export zip: %w", err)
	}

	hash := sha256.Sum256(zipData)
	hashHex := hex.EncodeToString(hash[:])

	timestamp := time.Now().UTC().Format("20060102T150405Z")
	objectKey := fmt.Sprintf("exports/tenants/%s/%s.zip", tenantID, timestamp)

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("export cancelled before upload: %w", err)
	}

	if err := uploader.PutObject(ctx, objectKey, zipData, "application/zip"); err != nil {
		return nil, fmt.Errorf("upload export: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("export cancelled after upload: %w", err)
	}

	presigned, err := uploader.PresignURL(ctx, objectKey, ExportPresignTTL24h)
	if err != nil {
		return nil, fmt.Errorf("presign url: %w", err)
	}

	return &TenantExportResult{
		ObjectKey:  objectKey,
		URL:        presigned.URL,
		ExpiresAt:  presigned.ExpiresAt,
		SHA256Hash: hashHex,
	}, nil
}

func buildExportZIP(ctx context.Context, plans []*repository.PlanRow, subs []*repository.SubscriptionRow, stmts []*repository.StatementRow) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := addJSONToZip(w, "plans.json", plans); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := addJSONToZip(w, "subscriptions.json", subs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := addJSONToZip(w, "statements.json", stmts); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func addJSONToZip(w *zip.Writer, name string, v interface{}) error {
	f, err := w.Create(name)
	if err != nil {
		return fmt.Errorf("create %s in zip: %w", name, err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s to zip: %w", name, err)
	}
	return nil
}

type ExportJobStatus string

const (
	ExportJobPending   ExportJobStatus = "pending"
	ExportJobRunning   ExportJobStatus = "running"
	ExportJobCompleted ExportJobStatus = "completed"
	ExportJobFailed    ExportJobStatus = "failed"
)

type ExportJob struct {
	ID          string              `json:"id"`
	TenantID    string              `json:"tenant_id"`
	CallerID    string              `json:"caller_id"`
	CallerRoles []string            `json:"caller_roles"`
	Status      ExportJobStatus     `json:"status"`
	Result      *TenantExportResult `json:"result,omitempty"`
	Error       string              `json:"error,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type ExportJobManager struct {
	jobs    map[string]*ExportJob
	pending chan *ExportJob
	svc     TenantExportService
	upload  s3.S3Uploader
	auditor *audit.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewExportJobManager(svc TenantExportService, uploader s3.S3Uploader, auditor *audit.Logger) *ExportJobManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &ExportJobManager{
		jobs:    make(map[string]*ExportJob),
		pending: make(chan *ExportJob, 100),
		svc:     svc,
		upload:  uploader,
		auditor: auditor,
		ctx:     ctx,
		cancel:  cancel,
	}
	m.wg.Add(1)
	go m.processLoop()
	return m
}

func (m *ExportJobManager) Stop() {
	m.cancel()
	m.wg.Wait()
}

// CreateJob enqueues a new export job for the given tenant, created by the
// identified caller with the specified roles. Returns ErrExportInProgress if
// the tenant already has a pending or running export.
func (m *ExportJobManager) CreateJob(ctx context.Context, tenantID, callerID string, callerRoles []string) (*ExportJob, error) {
	for _, existing := range m.jobs {
		if existing.TenantID == tenantID && (existing.Status == ExportJobPending || existing.Status == ExportJobRunning) {
			return nil, ErrExportInProgress
		}
	}

	if callerRoles == nil {
		callerRoles = []string{}
	}

	roles := make([]string, len(callerRoles))
	copy(roles, callerRoles)

	job := &ExportJob{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		CallerID:    callerID,
		CallerRoles: roles,
		Status:      ExportJobPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	m.jobs[job.ID] = job

	select {
	case m.pending <- job:
	case <-ctx.Done():
		delete(m.jobs, job.ID)
		return nil, ctx.Err()
	case <-m.ctx.Done():
		delete(m.jobs, job.ID)
		return nil, m.ctx.Err()
	}

	return job, nil
}

func (m *ExportJobManager) GetJob(id string) (*ExportJob, error) {
	job, ok := m.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return job, nil
}

func (m *ExportJobManager) processLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case job := <-m.pending:
			m.processJob(job)
		}
	}
}

func (m *ExportJobManager) processJob(job *ExportJob) {
	job.Status = ExportJobRunning
	job.UpdatedAt = time.Now().UTC()

	roles := job.CallerRoles
	if len(roles) == 0 {
		roles = []string{"admin"}
	}

	result, err := m.svc.ExportTenantData(m.ctx, job.CallerID, roles, job.TenantID, m.upload)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			job.Status = ExportJobFailed
			job.Error = "export cancelled: " + err.Error()
			job.UpdatedAt = time.Now().UTC()
			return
		}

		job.Status = ExportJobFailed
		job.Error = err.Error()
		job.UpdatedAt = time.Now().UTC()

		if m.auditor != nil {
			ctx := audit.WithActor(m.ctx, job.CallerID)
			_, _ = m.auditor.Log(ctx, audit.AuditEvent{
				Actor:    job.CallerID,
				Action:   "tenant_export",
				Resource: fmt.Sprintf("tenant:%s", job.TenantID),
				Outcome:  "failure",
				Metadata: map[string]interface{}{
					"job_id":  job.ID,
					"reason":  err.Error(),
				},
			})
		}
		return
	}

	job.Status = ExportJobCompleted
	job.Result = result
	job.UpdatedAt = time.Now().UTC()

	if m.auditor != nil {
		ctx := audit.WithActor(m.ctx, job.CallerID)
		_, _ = m.auditor.Log(ctx, audit.AuditEvent{
			Actor:    job.CallerID,
			Action:   "tenant_export",
			Resource: fmt.Sprintf("tenant:%s", job.TenantID),
			Outcome:  "success",
			Metadata: map[string]interface{}{
				"job_id":      job.ID,
				"object_key":  result.ObjectKey,
				"sha256_hash": result.SHA256Hash,
			},
		})
	}
}
