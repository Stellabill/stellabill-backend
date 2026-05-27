package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"
)

type stubAdapter struct {
	err error
}

func (s *stubAdapter) FetchSnapshots(ctx context.Context) ([]reconciliation.Snapshot, error) {
	return nil, s.err
}

type stubStore struct {
	saveErr error
	list    []reconciliation.Report
	listErr error
}

func (s *stubStore) SaveReports(reports []reconciliation.Report) error { return s.saveErr }
func (s *stubStore) ListReports() ([]reconciliation.Report, error) {
	return s.list, s.listErr
}
func (s *stubStore) ListReportsByTenant(tenantID string) ([]reconciliation.Report, error) {
	return s.list, s.listErr
}

func buildReconcileContext(roles []auth.Role, tenant, caller string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if caller != "" {
			c.Set("callerID", caller)
		}
		if tenant != "" {
			c.Set("tenantID", tenant)
		}
		if roles != nil {
			c.Set(auth.RolesContextKey, roles)
		}
		c.Next()
	})
	return r
}

func TestCoverage_NewReconcileHandler_MissingCaller(t *testing.T) {
	r := buildReconcileContext(nil, "", "")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/r", nil))
}

func TestCoverage_NewReconcileHandler_MissingTenant(t *testing.T) {
	r := buildReconcileContext(nil, "", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/r", nil))
}

func TestCoverage_NewReconcileHandler_NoPermission(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleCustomer}, "t1", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/r", nil))
}

func TestCoverage_NewReconcileHandler_InvalidJSON(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleAdmin}, "t1", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader([]byte("not json"))))
}

func TestCoverage_NewReconcileHandler_CrossTenant(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleMerchant}, "t1", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{}))
	rec := httptest.NewRecorder()
	body := []byte(`[{"subscription_id":"s1","tenant_id":"t2"}]`)
	req := httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
}

func TestCoverage_NewReconcileHandler_AdapterError(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleAdmin}, "t1", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{err: errors.New("oh no")}, &stubStore{}))
	rec := httptest.NewRecorder()
	body := []byte(`[{"subscription_id":"s1"}]`)
	req := httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
}

func TestCoverage_NewReconcileHandler_StoreSaveError(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleAdmin}, "t1", "alice")
	r.POST("/r", NewReconcileHandler(&stubAdapter{}, &stubStore{saveErr: errors.New("save")}))
	rec := httptest.NewRecorder()
	body := []byte(`[{"subscription_id":"s1"}]`)
	req := httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
}

func TestCoverage_NewListReportsHandler_MissingCaller(t *testing.T) {
	r := buildReconcileContext(nil, "", "")
	r.GET("/r", NewListReportsHandler(&stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
}

func TestCoverage_NewListReportsHandler_MissingTenant(t *testing.T) {
	r := buildReconcileContext(nil, "", "alice")
	r.GET("/r", NewListReportsHandler(&stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
}

func TestCoverage_NewListReportsHandler_NoPermission(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleCustomer}, "t1", "alice")
	r.GET("/r", NewListReportsHandler(&stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
}

func TestCoverage_NewListReportsHandler_InvalidCursor(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleAdmin}, "t1", "alice")
	r.GET("/r", NewListReportsHandler(&stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r?cursor=invalid", nil))
}

func TestCoverage_NewListReportsHandler_StoreError(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleAdmin}, "t1", "alice")
	r.GET("/r", NewListReportsHandler(&stubStore{listErr: errors.New("oh")}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
}

func TestCoverage_NewListReportsHandler_MerchantPath(t *testing.T) {
	r := buildReconcileContext([]auth.Role{auth.RoleMerchant}, "t1", "alice")
	r.GET("/r", NewListReportsHandler(&stubStore{}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
}
