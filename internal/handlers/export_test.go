package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/storage/s3"
)

// ---------------------------------------------------------------------------
// mock implementations
// ---------------------------------------------------------------------------

// exportMockSvc satisfies service.StatementService for export handler tests.
type exportMockSvc struct {
	exportResult *service.ExportResult
	exportErr    error
}

func (m *exportMockSvc) GetDetail(_ context.Context, _ string, _ []string, _ string) (*service.StatementDetail, []string, error) {
	return nil, nil, nil
}
func (m *exportMockSvc) ListByCustomer(_ context.Context, _ string, _ []string, _ string, _ repository.StatementQuery) (*service.ListStatementsDetail, int, []string, error) {
	return nil, 0, nil, nil
}
func (m *exportMockSvc) ExportStatements(_ context.Context, _ string, _ []string, _, _ string, _ s3.S3Uploader) (*service.ExportResult, error) {
	return m.exportResult, m.exportErr
}

// mockUploader is an S3Uploader that records calls and returns configured errors.
type mockUploader struct {
	putErr     error
	presignErr error
	putCalls   int
}

func (m *mockUploader) PutObject(_ context.Context, _ string, _ []byte, _ string) error {
	m.putCalls++
	return m.putErr
}
func (m *mockUploader) PresignURL(_ context.Context, key string, ttl time.Duration) (s3.PresignedURL, error) {
	if m.presignErr != nil {
		return s3.PresignedURL{}, m.presignErr
	}
	return s3.PresignedURL{
		URL:       "https://s3.example.com/" + key + "?sig=abc",
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func exportRouter(svc service.StatementService, uploader s3.S3Uploader, callerID string, roles []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", callerID)
		c.Set("roles", roles)
		c.Next()
	})
	r.POST("/api/admin/statements/export", NewExportStatementsHandler(svc, uploader))
	return r
}

func doExport(r *gin.Engine, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/statements/export", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestExportStatements_HappyPath_Admin(t *testing.T) {
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	svc := &exportMockSvc{
		exportResult: &service.ExportResult{
			ObjectKey: "exports/tenant-1/cust-1/20250101-120000.csv.gz",
			URL:       "https://s3.example.com/key?sig=abc",
			ExpiresAt: expiresAt,
		},
	}
	r := exportRouter(svc, &mockUploader{}, "admin-user", []string{"admin"})
	w := doExport(r, map[string]string{"tenant_id": "tenant-1", "customer_id": "cust-1"})

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "exports/tenant-1/cust-1/20250101-120000.csv.gz", resp["object_key"])
	assert.Contains(t, resp["url"].(string), "https://")
	assert.NotEmpty(t, resp["expires_at"])
}

func TestExportStatements_CrossTenant_Rejected(t *testing.T) {
	// merchant-A trying to export tenant merchant-B → ErrForbidden → 403.
	svc := &exportMockSvc{exportErr: service.ErrForbidden}
	r := exportRouter(svc, &mockUploader{}, "merchant-A", []string{"merchant"})
	w := doExport(r, map[string]string{"tenant_id": "merchant-B", "customer_id": "cust-1"})

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestExportStatements_MissingTenantID(t *testing.T) {
	svc := &exportMockSvc{}
	r := exportRouter(svc, &mockUploader{}, "admin", []string{"admin"})
	w := doExport(r, map[string]string{"customer_id": "cust-1"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant_id")
}

func TestExportStatements_MissingCustomerID(t *testing.T) {
	svc := &exportMockSvc{}
	r := exportRouter(svc, &mockUploader{}, "admin", []string{"admin"})
	w := doExport(r, map[string]string{"tenant_id": "tenant-1"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "customer_id")
}

func TestExportStatements_MissingBoth(t *testing.T) {
	svc := &exportMockSvc{}
	r := exportRouter(svc, &mockUploader{}, "admin", []string{"admin"})
	w := doExport(r, map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestExportStatements_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// No auth middleware → no caller_id in context.
	router.POST("/api/admin/statements/export", NewExportStatementsHandler(&exportMockSvc{}, &mockUploader{}))

	b, _ := json.Marshal(map[string]string{"tenant_id": "t", "customer_id": "c"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/statements/export", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExportStatements_S3_5xx_MapsToInternalError(t *testing.T) {
	// Service returns a wrapped upload error (simulates exhausted S3 retries).
	s3Err := fmt.Errorf("export: upload: %w", errors.New("s3: server error 503"))
	svc := &exportMockSvc{exportErr: s3Err}
	r := exportRouter(svc, &mockUploader{}, "admin", []string{"admin"})
	w := doExport(r, map[string]string{"tenant_id": "t", "customer_id": "c"})

	// Not a recognised service sentinel error → 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestExportStatements_NilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", "u1")
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	r.POST("/api/admin/statements/export", NewExportStatementsHandler(nil, &mockUploader{}))

	b, _ := json.Marshal(map[string]string{"tenant_id": "t", "customer_id": "c"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/statements/export", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestExportStatements_InvalidJSON(t *testing.T) {
	svc := &exportMockSvc{}
	r := exportRouter(svc, &mockUploader{}, "admin", []string{"admin"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/admin/statements/export", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
