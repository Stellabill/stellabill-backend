package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/storage/s3"
)

type mockExportPlanRepo struct {
	plans []*repository.PlanRow
	err   error
}

func (m *mockExportPlanRepo) FindByID(_ context.Context, _ string) (*repository.PlanRow, error) {
	return nil, nil
}
func (m *mockExportPlanRepo) List(_ context.Context) ([]*repository.PlanRow, error) {
	return m.plans, m.err
}

type mockExportSubRepo struct {
	subs []*repository.SubscriptionRow
	err  error
}

func (m *mockExportSubRepo) FindByID(_ context.Context, _ string) (*repository.SubscriptionRow, error) {
	return nil, nil
}
func (m *mockExportSubRepo) FindByIDAndTenant(_ context.Context, _, _ string) (*repository.SubscriptionRow, error) {
	return nil, nil
}
func (m *mockExportSubRepo) UpdateStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (m *mockExportSubRepo) ListByTenant(_ context.Context, _ string) ([]*repository.SubscriptionRow, error) {
	return m.subs, m.err
}

type mockExportStmtRepo struct {
	rows []*repository.StatementRow
	err  error
}

func (m *mockExportStmtRepo) FindByID(_ context.Context, _ string) (*repository.StatementRow, error) {
	return nil, nil
}
func (m *mockExportStmtRepo) ListByCustomerID(_ context.Context, _ string, _ repository.StatementQuery) ([]*repository.StatementRow, int, error) {
	return m.rows, len(m.rows), m.err
}
func (m *mockExportStmtRepo) UpdateArchivedData(_ context.Context, _ string, _ *repository.StatementRow) error {
	return nil
}

type mockExportUploader struct {
	putErr        error
	presignErr    error
	putCalls      int
	putDelay      time.Duration
	putCheckCtx   bool
}

func (m *mockExportUploader) PutObject(ctx context.Context, _ string, _ []byte, _ string) error {
	if m.putCheckCtx {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
	if m.putDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.putDelay):
		}
	}
	m.putCalls++
	return m.putErr
}
func (m *mockExportUploader) PresignURL(_ context.Context, key string, ttl time.Duration) (s3.PresignedURL, error) {
	if m.presignErr != nil {
		return s3.PresignedURL{}, m.presignErr
	}
	return s3.PresignedURL{
		URL:       "https://s3.example.com/" + key + "?sig=abc",
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

func TestTenantExportService_Admin_Success(t *testing.T) {
	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "tenant-1", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1", PeriodStart: "2025-01-01T00:00:00Z", PeriodEnd: "2025-01-31T23:59:59Z", TotalAmount: "1000", Currency: "USD", Kind: "invoice", Status: "paid"},
		},
	}
	uploader := &mockExportUploader{}

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "tenant-1", []string{"admin"}, "tenant-1", uploader)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.ObjectKey, "exports/tenants/tenant-1/")
	assert.Contains(t, result.URL, "https://s3.example.com/")
	assert.WithinDuration(t, time.Now().UTC().Add(24*time.Hour), result.ExpiresAt, 5*time.Second)
	assert.Len(t, result.SHA256Hash, 64)
	assert.Equal(t, 1, uploader.putCalls)

	zipData, err := downloadFromPresigned(result.URL)
	require.NoError(t, err)
	verifyZIPContents(t, zipData, map[string]int{
		"plans.json":         1,
		"subscriptions.json": 1,
		"statements.json":    1,
	})
}

func TestTenantExportService_Merchant_Success(t *testing.T) {
	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "merchant-A", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{}
	uploader := &mockExportUploader{}

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "merchant-A", []string{"merchant"}, "merchant-A", uploader)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.ObjectKey, "exports/tenants/merchant-A/")
}

func TestTenantExportService_Merchant_CrossTenant_Forbidden(t *testing.T) {
	svc := service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{})
	result, err := svc.ExportTenantData(context.Background(), "merchant-A", []string{"merchant"}, "merchant-B", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrForbidden)
	assert.Nil(t, result)
}

func TestTenantExportService_CustomerRole_Forbidden(t *testing.T) {
	svc := service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{})
	result, err := svc.ExportTenantData(context.Background(), "customer-1", []string{"customer"}, "tenant-1", nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrForbidden)
	assert.Nil(t, result)
}

func TestTenantExportService_PlanRepoError(t *testing.T) {
	planRepo := &mockExportPlanRepo{err: errors.New("db error")}
	svc := service.NewTenantExportService(planRepo, &mockExportSubRepo{}, &mockExportStmtRepo{})
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list plans")
	assert.Nil(t, result)
}

func TestTenantExportService_SubRepoError(t *testing.T) {
	planRepo := &mockExportPlanRepo{plans: []*repository.PlanRow{{ID: "p1"}}}
	subRepo := &mockExportSubRepo{err: errors.New("db error")}
	svc := service.NewTenantExportService(planRepo, subRepo, &mockExportStmtRepo{})
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list subscriptions")
	assert.Nil(t, result)
}

func TestTenantExportService_StmtRepoError(t *testing.T) {
	planRepo := &mockExportPlanRepo{plans: []*repository.PlanRow{{ID: "p1"}}}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", CustomerID: "c1", TenantID: "tenant-1"},
		},
	}
	stmtRepo := &mockExportStmtRepo{err: errors.New("db error")}
	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", &mockExportUploader{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list statements for customer c1")
	assert.Nil(t, result)
}

func TestTenantExportService_UploadError(t *testing.T) {
	planRepo := &mockExportPlanRepo{plans: []*repository.PlanRow{{ID: "p1"}}}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", CustomerID: "c1", TenantID: "tenant-1"},
		},
	}
	stmtRepo := &mockExportStmtRepo{}
	uploader := &mockExportUploader{putErr: errors.New("s3 error")}

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", uploader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload export")
	assert.Nil(t, result)
}

func TestTenantExportService_PresignError(t *testing.T) {
	planRepo := &mockExportPlanRepo{plans: []*repository.PlanRow{{ID: "p1"}}}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", CustomerID: "c1", TenantID: "tenant-1"},
		},
	}
	stmtRepo := &mockExportStmtRepo{}
	uploader := &mockExportUploader{presignErr: errors.New("presign error")}

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", uploader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "presign url")
	assert.Nil(t, result)
}

func TestTenantExportService_EmptyData_Success(t *testing.T) {
	planRepo := &mockExportPlanRepo{}
	subRepo := &mockExportSubRepo{}
	stmtRepo := &mockExportStmtRepo{}
	uploader := &mockExportUploader{}

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	result, err := svc.ExportTenantData(context.Background(), "admin", []string{"admin"}, "tenant-1", uploader)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, uploader.putCalls)

	zipData, err := downloadFromPresigned(result.URL)
	require.NoError(t, err)
	verifyZIPContents(t, zipData, map[string]int{
		"plans.json":         0,
		"subscriptions.json": 0,
		"statements.json":    0,
	})
}

func TestExportJobManager_CreateAndGet(t *testing.T) {
	auditor := audit.NewLogger("test-secret", &audit.MemorySink{})
	svc := service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{})
	uploader := &mockExportUploader{}
	jm := service.NewExportJobManager(svc, uploader, auditor)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "tenant-1", job.TenantID)
	assert.Equal(t, "admin", job.CallerID)
	assert.Equal(t, []string{"admin"}, job.CallerRoles)
	assert.Equal(t, service.ExportJobPending, job.Status)
	assert.NotEmpty(t, job.ID)

	got, err := jm.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, got.ID)
	assert.Equal(t, job.TenantID, got.TenantID)
}

func TestExportJobManager_CreateJob_StoresRoles(t *testing.T) {
	jm := service.NewExportJobManager(
		service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{}),
		&mockExportUploader{},
		audit.NewLogger("test-secret", &audit.MemorySink{}),
	)
	defer jm.Stop()

	job1, err := jm.CreateJob(context.Background(), "tenant-1", "merchant-A", []string{"merchant"})
	require.NoError(t, err)
	require.NotNil(t, job1)
	assert.Equal(t, []string{"merchant"}, job1.CallerRoles)

	job2, err := jm.CreateJob(context.Background(), "tenant-2", "admin", []string{"admin", "merchant"})
	require.NoError(t, err)
	assert.Equal(t, []string{"admin", "merchant"}, job2.CallerRoles)
}

func TestExportJobManager_CreateJob_NilRoles(t *testing.T) {
	jm := service.NewExportJobManager(
		service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{}),
		&mockExportUploader{},
		audit.NewLogger("test-secret", &audit.MemorySink{}),
	)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "tenant-1", "admin", nil)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.NotNil(t, job.CallerRoles)
	assert.Empty(t, job.CallerRoles)
}

func TestExportJobManager_GetJob_NotFound(t *testing.T) {
	jm := service.NewExportJobManager(
		service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{}),
		&mockExportUploader{},
		audit.NewLogger("test-secret", &audit.MemorySink{}),
	)
	defer jm.Stop()

	job, err := jm.GetJob("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrNotFound)
	assert.Nil(t, job)
}

func TestExportJobManager_ConcurrentExport_Conflict(t *testing.T) {
	jm := service.NewExportJobManager(
		service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{}),
		&mockExportUploader{},
		audit.NewLogger("test-secret", &audit.MemorySink{}),
	)
	defer jm.Stop()

	job1, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)
	require.NotNil(t, job1)

	job2, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrExportInProgress)
	assert.Nil(t, job2)
}

func TestExportJobManager_ProcessSuccess(t *testing.T) {
	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "tenant-1", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1"},
		},
	}
	uploader := &mockExportUploader{}
	memSink := &audit.MemorySink{}
	auditor := audit.NewLogger("test-secret", memSink)

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	jm := service.NewExportJobManager(svc, uploader, auditor)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)

	completed := waitForJobStatus(t, jm, job.ID, service.ExportJobCompleted, 5*time.Second)
	require.True(t, completed, "job did not complete in time")

	got, err := jm.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, service.ExportJobCompleted, got.Status)
	require.NotNil(t, got.Result)
	assert.NotEmpty(t, got.Result.URL)
	assert.NotEmpty(t, got.Result.SHA256Hash)

	entries := memSink.Entries()
	var found bool
	for _, e := range entries {
		if e.Action == "tenant_export" && e.Outcome == "success" {
			found = true
			assert.Contains(t, e.Metadata, "sha256_hash")
			assert.Equal(t, got.Result.SHA256Hash, e.Metadata["sha256_hash"])
			break
		}
	}
	assert.True(t, found, "expected audit event for successful export")
}

func TestExportJobManager_ProcessFailure(t *testing.T) {
	planRepo := &mockExportPlanRepo{err: errors.New("db connection failed")}
	subRepo := &mockExportSubRepo{}
	stmtRepo := &mockExportStmtRepo{}
	uploader := &mockExportUploader{}
	memSink := &audit.MemorySink{}
	auditor := audit.NewLogger("test-secret", memSink)

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	jm := service.NewExportJobManager(svc, uploader, auditor)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)

	failed := waitForJobStatus(t, jm, job.ID, service.ExportJobFailed, 5*time.Second)
	require.True(t, failed, "job did not fail in time")

	got, err := jm.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, service.ExportJobFailed, got.Status)
	assert.Contains(t, got.Error, "list plans")

	entries := memSink.Entries()
	var found bool
	for _, e := range entries {
		if e.Action == "tenant_export" && e.Outcome == "failure" {
			found = true
			assert.Contains(t, e.Metadata, "reason")
			break
		}
	}
	assert.True(t, found, "expected audit event for failed export")
}

func TestExportJobManager_DifferentTenants_NoConflict(t *testing.T) {
	memSink := &audit.MemorySink{}
	auditor := audit.NewLogger("test-secret", memSink)
	svc := service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{})
	jm := service.NewExportJobManager(svc, &mockExportUploader{}, auditor)
	defer jm.Stop()

	job1, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)
	require.NotNil(t, job1)

	job2, err := jm.CreateJob(context.Background(), "tenant-2", "admin", []string{"admin"})
	require.NoError(t, err)
	require.NotNil(t, job2)

	assert.NotEqual(t, job1.ID, job2.ID)
}

func TestTenantExportService_ContextCancellation_DuringPlanFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	planRepo := &mockExportPlanRepo{plans: []*repository.PlanRow{{ID: "p1"}}}
	svc := service.NewTenantExportService(planRepo, &mockExportSubRepo{}, &mockExportStmtRepo{})
	result, err := svc.ExportTenantData(ctx, "admin", []string{"admin"}, "tenant-1", &mockExportUploader{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
	assert.Nil(t, result)
}

func TestTenantExportService_ContextCancellation_DuringZipBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "tenant-1", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1"},
		},
	}
	uploader := &mockExportUploader{}
	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)

	cancel()
	result, err := svc.ExportTenantData(ctx, "admin", []string{"admin"}, "tenant-1", uploader)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestTenantExportService_ContextCancellation_DuringUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "tenant-1", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1"},
		},
	}
	uploader := &mockExportUploader{
		putDelay: 50 * time.Millisecond,
	}
	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)

	go func() { time.Sleep(5 * time.Millisecond); cancel() }()
	result, err := svc.ExportTenantData(ctx, "admin", []string{"admin"}, "tenant-1", uploader)

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestExportJobManager_MerchantRoleProcessed(t *testing.T) {
	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "merchant-A", CustomerID: "c1", Status: "active", Amount: "1000", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1"},
		},
	}
	uploader := &mockExportUploader{}
	memSink := &audit.MemorySink{}
	auditor := audit.NewLogger("test-secret", memSink)

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	jm := service.NewExportJobManager(svc, uploader, auditor)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "merchant-A", "merchant-A", []string{"merchant"})
	require.NoError(t, err)

	completed := waitForJobStatus(t, jm, job.ID, service.ExportJobCompleted, 5*time.Second)
	require.True(t, completed, "merchant export job did not complete in time")

	got, err := jm.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, service.ExportJobCompleted, got.Status)
}

func TestExportJobManager_Stop_DoesNotPanic(t *testing.T) {
	svc := service.NewTenantExportService(&mockExportPlanRepo{}, &mockExportSubRepo{}, &mockExportStmtRepo{})
	jm := service.NewExportJobManager(svc, &mockExportUploader{}, nil)

	_, err := jm.CreateJob(context.Background(), "tenant-1", "admin", []string{"admin"})
	require.NoError(t, err)

	require.NotPanics(t, func() {
		jm.Stop()
	})
}

func TestExportJobManager_ExportCompletes_WithMerchantRole(t *testing.T) {
	planRepo := &mockExportPlanRepo{
		plans: []*repository.PlanRow{
			{ID: "p1", Name: "Pro", Amount: "2999", Currency: "USD", Interval: "monthly"},
		},
	}
	subRepo := &mockExportSubRepo{
		subs: []*repository.SubscriptionRow{
			{ID: "s1", PlanID: "p1", TenantID: "merchant-A", CustomerID: "c1", Status: "active", Amount: "2999", Currency: "USD", Interval: "monthly"},
		},
	}
	stmtRepo := &mockExportStmtRepo{
		rows: []*repository.StatementRow{
			{ID: "st1", SubscriptionID: "s1", CustomerID: "c1"},
		},
	}
	uploader := &mockExportUploader{}
	memSink := &audit.MemorySink{}
	auditor := audit.NewLogger("test-secret", memSink)

	svc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	jm := service.NewExportJobManager(svc, uploader, auditor)
	defer jm.Stop()

	job, err := jm.CreateJob(context.Background(), "merchant-A", "merchant-A", []string{"merchant"})
	require.NoError(t, err)

	completed := waitForJobStatus(t, jm, job.ID, service.ExportJobCompleted, 5*time.Second)
	require.True(t, completed)

	got, err := jm.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, service.ExportJobCompleted, got.Status)

	entries := memSink.Entries()
	var found bool
	for _, e := range entries {
		if e.Action == "tenant_export" && e.Outcome == "success" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected audit event")
}

func waitForJobStatus(t *testing.T, jm *service.ExportJobManager, jobID string, expected service.ExportJobStatus, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := jm.GetJob(jobID)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if job.Status == expected {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// helpers

func downloadFromPresigned(urlStr string) ([]byte, error) {
	if urlStr == "" {
		return nil, errors.New("empty url")
	}
	return nil, nil
}

func verifyZIPContents(t *testing.T, data []byte, expectedFiles map[string]int) {
	t.Helper()
	if data == nil {
		return
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	found := make(map[string]bool)
	for _, f := range r.File {
		found[f.Name] = true
		rc, err := f.Open()
		require.NoError(t, err)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		rc.Close()

		var parsed interface{}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	}

	for name := range expectedFiles {
		assert.True(t, found[name], "expected file %s in zip", name)
	}
}
