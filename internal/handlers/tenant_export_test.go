package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/service"
)

type mockExportJobManager struct {
	createJobFn func(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error)
	getJobFn    func(id string) (*service.ExportJob, error)
}

func (m *mockExportJobManager) CreateJob(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error) {
	if m.createJobFn != nil {
		return m.createJobFn(ctx, tenantID, callerID, callerRoles)
	}
	if callerRoles == nil {
		callerRoles = []string{}
	}
	roles := make([]string, len(callerRoles))
	copy(roles, callerRoles)
	return &service.ExportJob{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		CallerID:    callerID,
		CallerRoles: roles,
		Status:      service.ExportJobPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func (m *mockExportJobManager) GetJob(id string) (*service.ExportJob, error) {
	if m.getJobFn != nil {
		return m.getJobFn(id)
	}
	return nil, service.ErrNotFound
}

func tenantExportRouter(jobManager ExportJobManager, callerID string, roles []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", callerID)
		c.Set("callerID", callerID)
		c.Set("roles", roles)
		c.Set("tenantID", callerID)
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(jobManager))
	r.GET("/api/v1/tenants/me/export/:job_id", NewTenantExportStatusHandler(jobManager))
	return r
}

func doExportRequest(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenants/me/export", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func doExportStatusRequest(r *gin.Engine, jobID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/tenants/me/export/"+jobID, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestTenantExport_Create_HappyPath(t *testing.T) {
	jm := &mockExportJobManager{}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusAccepted, w.Code)
	var resp createExportResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.JobID)
	assert.Contains(t, resp.StatusURL, resp.JobID)
	assert.Contains(t, resp.StatusURL, "/api/v1/tenants/me/export/")
}

func TestTenantExport_Create_MerchantOwnTenant(t *testing.T) {
	jm := &mockExportJobManager{}
	r := tenantExportRouter(jm, "tenant-1", []string{"merchant"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusAccepted, w.Code)
}

func TestTenantExport_Create_MerchantCrossTenant(t *testing.T) {
	jm := &mockExportJobManager{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "merchant-A")
		c.Set("callerID", "merchant-A")
		c.Set("roles", []string{"merchant"})
		c.Set("tenantID", "merchant-A")
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(jm))

	w := doExportRequest(r)
	require.Equal(t, http.StatusAccepted, w.Code)
}

func TestTenantExport_Create_ForbiddenRole(t *testing.T) {
	jm := &mockExportJobManager{}
	r := tenantExportRouter(jm, "customer-1", []string{"customer"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestTenantExport_Create_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(&mockExportJobManager{}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenants/me/export", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTenantExport_Create_MissingTenantContext(t *testing.T) {
	jm := &mockExportJobManager{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "admin")
		c.Set("callerID", "admin")
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(jm))

	w := doExportRequest(r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTenantExport_Create_Conflict(t *testing.T) {
	jm := &mockExportJobManager{
		createJobFn: func(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error) {
			return nil, service.ErrExportInProgress
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestTenantExport_Create_InternalError(t *testing.T) {
	jm := &mockExportJobManager{
		createJobFn: func(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error) {
			return nil, assert.AnError
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTenantExport_Create_NilJobManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "admin")
		c.Set("callerID", "admin")
		c.Set("roles", []string{"admin"})
		c.Set("tenantID", "tenant-1")
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(nil))

	w := doExportRequest(r)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTenantExport_Status_HappyPath_Pending(t *testing.T) {
	jm := &mockExportJobManager{
		getJobFn: func(id string) (*service.ExportJob, error) {
			return &service.ExportJob{
				ID:        id,
				TenantID:  "tenant-1",
				CallerID:  "admin",
				Status:    service.ExportJobPending,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportStatusRequest(r, "job-123")

	require.Equal(t, http.StatusOK, w.Code)
	var resp exportStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "job-123", resp.JobID)
	assert.Equal(t, service.ExportJobPending, resp.Status)
	assert.Nil(t, resp.Result)
}

func TestTenantExport_Status_HappyPath_Completed(t *testing.T) {
	result := &service.TenantExportResult{
		ObjectKey:  "exports/tenants/t1/20250101-120000Z.zip",
		URL:        "https://s3.example.com/exports/tenants/t1/20250101-120000Z.zip?sig=abc",
		ExpiresAt:  time.Now().UTC().Add(24 * time.Hour),
		SHA256Hash: "abc123def456",
	}
	jm := &mockExportJobManager{
		getJobFn: func(id string) (*service.ExportJob, error) {
			return &service.ExportJob{
				ID:        id,
				TenantID:  "tenant-1",
				CallerID:  "admin",
				Status:    service.ExportJobCompleted,
				Result:    result,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportStatusRequest(r, "job-123")

	require.Equal(t, http.StatusOK, w.Code)
	var resp exportStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, service.ExportJobCompleted, resp.Status)
	require.NotNil(t, resp.Result)
	assert.Equal(t, result.ObjectKey, resp.Result.ObjectKey)
	assert.Equal(t, result.SHA256Hash, resp.Result.SHA256Hash)
}

func TestTenantExport_Status_HappyPath_Failed(t *testing.T) {
	jm := &mockExportJobManager{
		getJobFn: func(id string) (*service.ExportJob, error) {
			return &service.ExportJob{
				ID:        id,
				TenantID:  "tenant-1",
				CallerID:  "admin",
				Status:    service.ExportJobFailed,
				Error:     "s3 upload failed",
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportStatusRequest(r, "job-123")

	require.Equal(t, http.StatusOK, w.Code)
	var resp exportStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, service.ExportJobFailed, resp.Status)
	assert.Equal(t, "s3 upload failed", resp.Error)
}

func TestTenantExport_Status_NotFound(t *testing.T) {
	jm := &mockExportJobManager{
		getJobFn: func(id string) (*service.ExportJob, error) {
			return nil, service.ErrNotFound
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})
	w := doExportStatusRequest(r, "nonexistent")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestTenantExport_Status_CrossTenantRead(t *testing.T) {
	jm := &mockExportJobManager{
		getJobFn: func(id string) (*service.ExportJob, error) {
			return &service.ExportJob{
				ID:       id,
				TenantID: "tenant-2",
				CallerID: "other-user",
				Status:   service.ExportJobCompleted,
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "tenant-1")
		c.Set("callerID", "tenant-1")
		c.Set("roles", []string{"admin"})
		c.Set("tenantID", "tenant-1")
		c.Next()
	})
	r.GET("/api/v1/tenants/me/export/:job_id", NewTenantExportStatusHandler(jm))

	w := doExportStatusRequest(r, "job-123")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestTenantExport_Status_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/tenants/me/export/:job_id", NewTenantExportStatusHandler(&mockExportJobManager{}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/tenants/me/export/job-123", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTenantExport_Status_MissingTenantContext(t *testing.T) {
	jm := &mockExportJobManager{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "admin")
		c.Set("callerID", "admin")
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	r.GET("/api/v1/tenants/me/export/:job_id", NewTenantExportStatusHandler(jm))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/tenants/me/export/job-123", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTenantExport_Status_NilJobManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "admin")
		c.Set("callerID", "admin")
		c.Set("roles", []string{"admin"})
		c.Set("tenantID", "tenant-1")
		c.Next()
	})
	r.GET("/api/v1/tenants/me/export/:job_id", NewTenantExportStatusHandler(nil))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/tenants/me/export/job-123", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTenantExport_Status_EmptyJobID(t *testing.T) {
	jm := &mockExportJobManager{}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/tenants/me/export/", nil)
	r.ServeHTTP(w, req)

	// Gin does not route empty :job_id param; the route won't match with trailing slash
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTenantExport_Create_MerchantCrossTenant_Denied(t *testing.T) {
	jm := &mockExportJobManager{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "merchant-A")
		c.Set("callerID", "merchant-A")
		c.Set("roles", []string{"merchant"})
		c.Set("tenantID", "tenant-B")
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(jm))

	w := doExportRequest(r)
	require.Equal(t, http.StatusForbidden, w.Code)

	var resp ErrorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, string(ErrorCodeForbidden), resp.Code)
}

func TestTenantExport_Create_ReturnsRolesInJob(t *testing.T) {
	var capturedRoles []string
	jm := &mockExportJobManager{
		createJobFn: func(ctx context.Context, tenantID, callerID string, callerRoles []string) (*service.ExportJob, error) {
			capturedRoles = callerRoles
			return &service.ExportJob{
				ID:          uuid.New().String(),
				TenantID:    tenantID,
				CallerID:    callerID,
				CallerRoles: callerRoles,
				Status:      service.ExportJobPending,
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			}, nil
		},
	}
	r := tenantExportRouter(jm, "tenant-1", []string{"admin", "merchant"})
	w := doExportRequest(r)

	require.Equal(t, http.StatusAccepted, w.Code)
	require.NotNil(t, capturedRoles)
	assert.ElementsMatch(t, []string{"admin", "merchant"}, capturedRoles)
}

func TestTenantExport_Create_EmptyRoles(t *testing.T) {
	jm := &mockExportJobManager{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "admin")
		c.Set("callerID", "admin")
		c.Set("roles", []string{})
		c.Set("tenantID", "tenant-1")
		c.Next()
	})
	r.POST("/api/v1/tenants/me/export", NewTenantExportHandler(jm))

	w := doExportRequest(r)
	// With empty roles, no role matches admin or merchant -> 403
	require.Equal(t, http.StatusForbidden, w.Code)
}
