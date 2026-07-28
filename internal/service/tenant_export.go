package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
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
			Limit: statementsPageSize,
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

type ExportOperationStatus string

const (
	OperationPending   ExportOperationStatus = "pending"
	OperationRunning   ExportOperationStatus = "running"
	OperationSucceeded ExportOperationStatus = "succeeded"
	OperationFailed    ExportOperationStatus = "failed"
)

type ExportJob struct {
	ID          string              `json:"id"`
	OperationID string              `json:"operation_id,omitempty"`
	TenantID    string              `json:"tenant_id"`
	CallerID    string              `json:"caller_id"`
	CallerRoles []string            `json:"caller_roles"`
	Status      ExportJobStatus     `json:"status"`
	Result      *TenantExportResult `json:"result,omitempty"`
	Error       string              `json:"error,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type ExportOperation struct {
	ID          string                `json:"id"`
	TenantID    string                `json:"tenant_id"`
	CallerID    string                `json:"caller_id"`
	CallerRoles []string              `json:"caller_roles"`
	Status      ExportOperationStatus `json:"status"`
	Result      *TenantExportResult   `json:"result,omitempty"`
	Error       string                `json:"error,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type ExportOperationStore interface {
	Create(ctx context.Context, op *ExportOperation) error
	Get(ctx context.Context, id string) (*ExportOperation, error)
	Update(ctx context.Context, op *ExportOperation) error
	DeleteExpired(ctx context.Context, before time.Time) error
}

type inMemoryExportOperationStore struct {
	mu      sync.RWMutex
	ops     map[string]*ExportOperation
	ttl     time.Duration
	created time.Time
}

type postgresExportOperationStore struct {
	db  *sql.DB
	ttl time.Duration
}

func NewInMemoryExportOperationStore(ttl time.Duration) ExportOperationStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &inMemoryExportOperationStore{ops: make(map[string]*ExportOperation), ttl: ttl, created: time.Now().UTC()}
}

func (s *inMemoryExportOperationStore) Create(_ context.Context, op *ExportOperation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ops == nil {
		s.ops = make(map[string]*ExportOperation)
	}
	s.ops[op.ID] = op
	return nil
}

func (s *inMemoryExportOperationStore) Get(_ context.Context, id string) (*ExportOperation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	op, ok := s.ops[id]
	if !ok {
		return nil, ErrNotFound
	}
	return op, nil
}

func (s *inMemoryExportOperationStore) Update(_ context.Context, op *ExportOperation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops[op.ID] = op
	return nil
}

func (s *inMemoryExportOperationStore) DeleteExpired(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, op := range s.ops {
		if op.UpdatedAt.Before(before) || op.CreatedAt.Before(before) {
			delete(s.ops, id)
		}
	}
	return nil
}

func NewPostgresExportOperationStore(db *sql.DB, ttl time.Duration) ExportOperationStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &postgresExportOperationStore{db: db, ttl: ttl}
}

func (s *postgresExportOperationStore) Create(ctx context.Context, op *ExportOperation) error {
	if s.db == nil {
		return errors.New("database connection unavailable")
	}
	rolesJSON, err := json.Marshal(op.CallerRoles)
	if err != nil {
		return err
	}
	var resultJSON []byte
	if op.Result != nil {
		resultJSON, err = json.Marshal(op.Result)
		if err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO export_operations (id, tenant_id, caller_id, caller_roles, status, result, error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, op.ID, op.TenantID, op.CallerID, string(rolesJSON), string(op.Status), string(resultJSON), op.Error, op.CreatedAt, op.UpdatedAt)
	return err
}

func (s *postgresExportOperationStore) Get(ctx context.Context, id string) (*ExportOperation, error) {
	if s.db == nil {
		return nil, errors.New("database connection unavailable")
	}
	var op ExportOperation
	var callerRoles []byte
	var resultJSON []byte
	var status string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, caller_id, caller_roles, status, result, error, created_at, updated_at
		FROM export_operations
		WHERE id = $1
	`, id).Scan(&op.ID, &op.TenantID, &op.CallerID, &callerRoles, &status, &resultJSON, &op.Error, &op.CreatedAt, &op.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	op.Status = ExportOperationStatus(status)
	if len(callerRoles) > 0 {
		if err := json.Unmarshal(callerRoles, &op.CallerRoles); err != nil {
			return nil, err
		}
	}
	if len(resultJSON) > 0 {
		if err := json.Unmarshal(resultJSON, &op.Result); err != nil {
			return nil, err
		}
	}
	return &op, nil
}

func (s *postgresExportOperationStore) Update(ctx context.Context, op *ExportOperation) error {
	if s.db == nil {
		return errors.New("database connection unavailable")
	}
	rolesJSON, err := json.Marshal(op.CallerRoles)
	if err != nil {
		return err
	}
	var resultJSON []byte
	if op.Result != nil {
		resultJSON, err = json.Marshal(op.Result)
		if err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE export_operations
		SET tenant_id = $2, caller_id = $3, caller_roles = $4, status = $5, result = $6, error = $7, updated_at = $8
		WHERE id = $1
	`, op.ID, op.TenantID, op.CallerID, string(rolesJSON), string(op.Status), string(resultJSON), op.Error, op.UpdatedAt)
	return err
}

func (s *postgresExportOperationStore) DeleteExpired(ctx context.Context, before time.Time) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM export_operations
		WHERE created_at < $1 OR updated_at < $1
	`, before)
	return err
}

type ExportJobManager struct {
	jobs       map[string]*ExportJob
	pending    chan *ExportJob
	svc        TenantExportService
	upload     s3.S3Uploader
	auditor    *audit.Logger
	operations ExportOperationStore
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewExportJobManager(svc TenantExportService, uploader s3.S3Uploader, auditor *audit.Logger) *ExportJobManager {
	return NewExportJobManagerWithOperationStore(svc, uploader, auditor, NewInMemoryExportOperationStore(24*time.Hour))
}

func NewExportJobManagerWithOperationStore(svc TenantExportService, uploader s3.S3Uploader, auditor *audit.Logger, operations ExportOperationStore) *ExportJobManager {
	ctx, cancel := context.WithCancel(context.Background())
	if operations == nil {
		operations = NewInMemoryExportOperationStore(24 * time.Hour)
	}
	m := &ExportJobManager{
		jobs:       make(map[string]*ExportJob),
		pending:    make(chan *ExportJob, 100),
		svc:        svc,
		upload:     uploader,
		auditor:    auditor,
		operations: operations,
		ctx:        ctx,
		cancel:     cancel,
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
	operation := &ExportOperation{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		CallerID:    callerID,
		CallerRoles: roles,
		Status:      OperationPending,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}
	if err := m.operations.Create(ctx, operation); err != nil {
		return nil, err
	}
	job.OperationID = operation.ID
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

func (m *ExportJobManager) CreateOperation(ctx context.Context, tenantID, callerID string, callerRoles []string) (*ExportOperation, error) {
	before := time.Now().UTC().Add(-24 * time.Hour)
	_ = m.operations.DeleteExpired(ctx, before)
	if callerRoles == nil {
		callerRoles = []string{}
	}
	roles := make([]string, len(callerRoles))
	copy(roles, callerRoles)
	operation := &ExportOperation{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		CallerID:    callerID,
		CallerRoles: roles,
		Status:      OperationPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := m.operations.Create(ctx, operation); err != nil {
		return nil, err
	}
	return operation, nil
}

func (m *ExportJobManager) GetOperation(id string) (*ExportOperation, error) {
	before := time.Now().UTC().Add(-24 * time.Hour)
	_ = m.operations.DeleteExpired(context.Background(), before)
	return m.operations.Get(context.Background(), id)
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
	if job.OperationID != "" {
		if op, err := m.operations.Get(m.ctx, job.OperationID); err == nil {
			op.Status = OperationRunning
			op.UpdatedAt = job.UpdatedAt
			_ = m.operations.Update(m.ctx, op)
		}
	}

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
			if job.OperationID != "" {
				if op, err := m.operations.Get(m.ctx, job.OperationID); err == nil {
					op.Status = OperationFailed
					op.Error = job.Error
					op.UpdatedAt = job.UpdatedAt
					_ = m.operations.Update(m.ctx, op)
				}
			}
			return
		}

		job.Status = ExportJobFailed
		job.Error = err.Error()
		job.UpdatedAt = time.Now().UTC()
		if job.OperationID != "" {
			if op, err := m.operations.Get(m.ctx, job.OperationID); err == nil {
				op.Status = OperationFailed
				op.Error = job.Error
				op.UpdatedAt = job.UpdatedAt
				_ = m.operations.Update(m.ctx, op)
			}
		}

		if m.auditor != nil {
			ctx := audit.WithActor(m.ctx, job.CallerID)
			_, _ = m.auditor.Log(ctx, audit.AuditEvent{
				Actor:    job.CallerID,
				Action:   "tenant_export",
				Resource: fmt.Sprintf("tenant:%s", job.TenantID),
				Outcome:  "failure",
				Metadata: map[string]interface{}{
					"job_id": job.ID,
					"reason": err.Error(),
				},
			})
		}
		return
	}

	job.Status = ExportJobCompleted
	job.Result = result
	job.UpdatedAt = time.Now().UTC()
	if job.OperationID != "" {
		if op, err := m.operations.Get(m.ctx, job.OperationID); err == nil {
			op.Status = OperationSucceeded
			op.Result = result
			op.UpdatedAt = job.UpdatedAt
			_ = m.operations.Update(m.ctx, op)
		}
	}

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
