package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/reconciliation"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// injectContextMiddleware sets role and tenantID on the gin context so that
// RequirePermission and the handler can read them without a real JWT.
func injectContextMiddleware(role, tenantID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if role != "" {
			// RequirePermission reads the X-Role header to derive the role.
			c.Request.Header.Set("X-Role", role)
		}
		c.Set("tenantID", tenantID)
		c.Set("callerID", "test-user-id")
		c.Next()
	}
}

// failingAdapter always returns an error from FetchSnapshots.
type failingAdapter struct{ err error }

func (f *failingAdapter) FetchSnapshots(_ context.Context) ([]reconciliation.Snapshot, error) {
	return nil, f.err
}

// failingStore always returns an error from SaveReports.
type failingStore struct{ err error }

func (f *failingStore) SaveReports(_ []reconciliation.Report) error { return f.err }
func (f *failingStore) ListReports() ([]reconciliation.Report, error) {
	return nil, nil
}
func (f *failingStore) ListReportsByTenant(_ string) ([]reconciliation.Report, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupReconcileRouter(adapter reconciliation.Adapter, store reconciliation.Store, tenantID string, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectContextMiddleware(role, tenantID))
	r.POST("/reconcile",
		auth.RequirePermission(auth.PermManageReconciliation),
		NewReconcileHandler(adapter, store),
	)
	r.GET("/reports",
		auth.RequirePermission(auth.PermReadReconciliation),
		NewListReportsHandler(store),
	)
	return r
}

func buildReconcileRouter(
	role, tenantID string,
	adapter reconciliation.Adapter,
	store reconciliation.Store,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectContextMiddleware(role, tenantID))
	r.POST("/reconcile",
		auth.RequirePermission(auth.PermManageReconciliation),
		NewReconcileHandler(adapter, store),
	)
	return r
}

// ---------------------------------------------------------------------------
// Primary table-driven test: RBAC & tenant scoping
// ---------------------------------------------------------------------------

func TestReconcileHandler_TenantScopingAndRBAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	tests := []struct {
		name              string
		role              string
		contextTenant     string
		backendSubs       []reconciliation.BackendSubscription
		snapshots         []reconciliation.Snapshot
		expectedCode      int
		expectedReports   int
		expectedTenantIDs []string // checked against stored AND response reports
	}{
		{
			// Test 2: Admin full access — sees all tenant data without restriction.
			name:          "Admin can view multiple tenants and explicitly set tenant",
			role:          string(auth.RoleAdmin),
			contextTenant: "admin-tenant",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", UpdatedAt: now},
				{SubscriptionID: "sub-2", TenantID: "tenant-b", Status: "active", UpdatedAt: now},
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-2", TenantID: "tenant-b", Status: "active", ExportedAt: now},
			},
			expectedCode:      http.StatusOK,
			expectedReports:   2,
			expectedTenantIDs: []string{"tenant-a", "tenant-b"},
		},
		{
			// Test 3: Tenant stamping — empty TenantID stamped with context tenant.
			name:          "Merchant can reconcile own tenant and stamps empty TenantID",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "merchant-a", Status: "active", UpdatedAt: now},
				{SubscriptionID: "sub-2", TenantID: "", Status: "active", UpdatedAt: now}, // must be stamped
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "merchant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-2", TenantID: "merchant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-other", TenantID: "merchant-b", Status: "active", ExportedAt: now}, // filtered
			},
			expectedCode:      http.StatusOK,
			expectedReports:   2,
			expectedTenantIDs: []string{"merchant-a", "merchant-a"},
		},
		{
			// Test 1: Merchant cross-tenant access MUST FAIL with 403.
			name:          "Merchant cannot reconcile cross-tenant subscription",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "merchant-b", Status: "active", UpdatedAt: now},
			},
			snapshots:       []reconciliation.Snapshot{},
			expectedCode:    http.StatusForbidden,
			expectedReports: 0,
		},
		{
			// Test 4: Snapshot filtering — non-admin only sees their own tenant snapshots.
			name:          "Merchant sees only own snapshots (cross-tenant snapshot filtered)",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-b", TenantID: "", Status: "active", UpdatedAt: now}, // stamped merchant-a
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-b", TenantID: "merchant-b", Status: "active", ExportedAt: now}, // filtered out
			},
			expectedCode:      http.StatusOK,
			expectedReports:   1, // 1 mismatch report: snapshot missing
			expectedTenantIDs: []string{"merchant-a"},
		},
		{
			// Test 5: Missing manage:reconciliation permission → 403.
			name:          "RoleUser missing manage:reconciliation permission returns 403",
			role:          string(auth.RoleUser),
			contextTenant: "tenant-user",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-user", Status: "active", UpdatedAt: now},
			},
			snapshots:       []reconciliation.Snapshot{},
			expectedCode:    http.StatusForbidden,
			expectedReports: 0,
		},
		{
			// Test 5b: Missing tenant context for non-admin → 403.
			name:          "Merchant with missing tenantID in context is rejected",
			role:          string(auth.RoleMerchant),
			contextTenant: "", // no tenant in context
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "", Status: "active", UpdatedAt: now},
			},
			snapshots:       []reconciliation.Snapshot{},
			expectedCode:    http.StatusForbidden,
			expectedReports: 0,
		},
		{
			// Test 5c: Admin with empty context tenant still works (admin bypass).
			name:          "Admin with empty context tenantID still processes (no restriction)",
			role:          string(auth.RoleAdmin),
			contextTenant: "",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", UpdatedAt: now},
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", ExportedAt: now},
			},
			expectedCode:      http.StatusOK,
			expectedReports:   1,
			expectedTenantIDs: []string{"tenant-a"},
		},
		{
			// Test 6: Malformed JSON → 400.
			name:          "Malformed JSON request body returns 400",
			role:          string(auth.RoleAdmin),
			contextTenant: "admin",
			backendSubs:   nil, // signal: send raw bad JSON
			snapshots:     []reconciliation.Snapshot{},
			expectedCode:  http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := reconciliation.NewMemoryAdapter(tc.snapshots...)
			store := reconciliation.NewMemoryStore()

			r := buildReconcileRouter(tc.role, tc.contextTenant, adapter, store)

			// Build request body.
			var payload []byte
			if tc.name == "Malformed JSON request body returns 400" {
				payload = []byte("{bad-json}")
			} else {
				payload, _ = json.Marshal(tc.backendSubs)
			}

			req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Fatalf("expected HTTP %d, got %d; body: %s", tc.expectedCode, w.Code, w.Body.String())
			}

			if w.Code != http.StatusOK {
				return // non-200 cases don't need further assertions
			}

			// Parse response.
			var resp struct {
				Summary struct {
					Total      int `json:"total"`
					Matched    int `json:"matched"`
					Mismatched int `json:"mismatched"`
				} `json:"summary"`
				Reports []reconciliation.Report `json:"reports"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse response JSON: %v", err)
			}

			// Validate report count.
			if len(resp.Reports) != tc.expectedReports {
				t.Errorf("expected %d reports in response, got %d", tc.expectedReports, len(resp.Reports))
			}

			// Validate summary totals.
			if resp.Summary.Total != tc.expectedReports {
				t.Errorf("expected summary.total=%d, got %d", tc.expectedReports, resp.Summary.Total)
			}

			// Validate stored reports (tenant stamping).
			saved, err := store.ListReports()
			if err != nil {
				t.Fatalf("store.ListReports() error: %v", err)
			}
			if len(saved) != tc.expectedReports {
				t.Errorf("expected %d saved reports, got %d", tc.expectedReports, len(saved))
			}

			// Validate tenant IDs on both stored and response reports.
			for i, expectedTenant := range tc.expectedTenantIDs {
				if i >= len(saved) {
					t.Errorf("saved[%d] missing", i)
					continue
				}
				if saved[i].TenantID != expectedTenant {
					t.Errorf("stored report[%d]: expected tenantID=%q, got %q",
						i, expectedTenant, saved[i].TenantID)
				}
				if i < len(resp.Reports) && resp.Reports[i].TenantID != expectedTenant {
					t.Errorf("response report[%d]: expected tenantID=%q, got %q",
						i, expectedTenant, resp.Reports[i].TenantID)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 7: Store failure handling — SaveReports error propagated via header.
// ---------------------------------------------------------------------------

func TestReconcileHandler_StoreFailure_PropagatesViaHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	saveErr := errors.New("disk full")
	store := &failingStore{err: saveErr}
	adapter := reconciliation.NewMemoryAdapter(
		reconciliation.Snapshot{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", ExportedAt: now},
	)

	r := buildReconcileRouter(string(auth.RoleAdmin), "t1", adapter, store)

	body, _ := json.Marshal([]reconciliation.BackendSubscription{
		{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", UpdatedAt: now},
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Request must still succeed (best-effort save).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Error must be surfaced via the X-Reconcile-Save-Error header.
	header := w.Header().Get("X-Reconcile-Save-Error")
	if header == "" {
		t.Fatal("expected X-Reconcile-Save-Error header to be set, but it was empty")
	}
	if header != saveErr.Error() {
		t.Errorf("expected header value %q, got %q", saveErr.Error(), header)
	}
}

// ---------------------------------------------------------------------------
// Test 8: Adapter failure — FetchSnapshots error → 500.
// ---------------------------------------------------------------------------

func TestReconcileHandler_AdapterFailure_Returns500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	fetchErr := errors.New("upstream timeout")
	adapter := &failingAdapter{err: fetchErr}
	store := reconciliation.NewMemoryStore()

	r := buildReconcileRouter(string(auth.RoleAdmin), "t1", adapter, store)

	body, _ := json.Marshal([]reconciliation.BackendSubscription{
		{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", UpdatedAt: now},
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp["error"] != "failed to fetch snapshots" {
		t.Errorf("expected error message 'failed to fetch snapshots', got %q", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// Test: nil store path — handler works without a store.
// ---------------------------------------------------------------------------

func TestReconcileHandler_NilStore_SucceedsWithoutPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	adapter := reconciliation.NewMemoryAdapter(
		reconciliation.Snapshot{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", ExportedAt: now},
	)

	// Pass nil store — handler must not panic.
	r := buildReconcileRouter(string(auth.RoleAdmin), "t1", adapter, nil)

	body, _ := json.Marshal([]reconciliation.BackendSubscription{
		{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", UpdatedAt: now},
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil store, got %d; body: %s", w.Code, w.Body.String())
	}
	// X-Reconcile-Save-Error must NOT be set (no save attempted).
	if h := w.Header().Get("X-Reconcile-Save-Error"); h != "" {
		t.Errorf("expected no X-Reconcile-Save-Error header, got %q", h)
	}
}

// ---------------------------------------------------------------------------
// Test: matched counter — verify summary.matched is incremented correctly.
// ---------------------------------------------------------------------------

func TestReconcileHandler_SummaryMatchedCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	// sub-1 matches, sub-2 has a status mismatch.
	adapter := reconciliation.NewMemoryAdapter(
		reconciliation.Snapshot{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", ExportedAt: now},
		reconciliation.Snapshot{SubscriptionID: "sub-2", TenantID: "t1", Status: "canceled", ExportedAt: now},
	)
	store := reconciliation.NewMemoryStore()
	r := buildReconcileRouter(string(auth.RoleAdmin), "t1", adapter, store)

	body, _ := json.Marshal([]reconciliation.BackendSubscription{
		{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", UpdatedAt: now},
		{SubscriptionID: "sub-2", TenantID: "t1", Status: "active", UpdatedAt: now}, // mismatch: backend active, contract canceled
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summary struct {
			Total      int `json:"total"`
			Matched    int `json:"matched"`
			Mismatched int `json:"mismatched"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", resp.Summary.Total)
	}
	if resp.Summary.Matched != 1 {
		t.Errorf("expected matched=1, got %d", resp.Summary.Matched)
	}
	if resp.Summary.Mismatched != 1 {
		t.Errorf("expected mismatched=1, got %d", resp.Summary.Mismatched)
	}
}

// ---------------------------------------------------------------------------
// Test: no callerID — exercise role path for string type (non-auth.Role).
// ---------------------------------------------------------------------------

func TestReconcileHandler_RoleAsString_Merchant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	// Inject role as plain string (not auth.Role type) to test the string branch.
	gin.SetMode(gin.TestMode)
	adapter := reconciliation.NewMemoryAdapter(
		reconciliation.Snapshot{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", ExportedAt: now},
	)
	store := reconciliation.NewMemoryStore()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request.Header.Set("X-Role", string(auth.RoleMerchant))
		c.Set(auth.RoleContextKey, string(auth.RoleMerchant)) // string, not auth.Role
		c.Set("tenantID", "t1")
		c.Next()
	})
	r.POST("/reconcile",
		auth.RequirePermission(auth.PermManageReconciliation),
		NewReconcileHandler(adapter, store),
	)

	body, _ := json.Marshal([]reconciliation.BackendSubscription{
		{SubscriptionID: "sub-1", TenantID: "t1", Status: "active", UpdatedAt: now},
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Test ListReportsHandler tests
// ---------------------------------------------------------------------------

func TestListReportsHandler_TenantIsolation(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	// Add reports for two different tenants
	store.SaveReports([]reconciliation.Report{
		{SubscriptionID: "sub-1", TenantID: "tenant-a"},
		{SubscriptionID: "sub-2", TenantID: "tenant-b"},
	})

	// Test for tenant-a
	r := setupReconcileRouter(nil, store, "tenant-a", string(auth.RoleMerchant))
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Reports) != 1 {
		t.Errorf("expected 1 report, got %d", len(resp.Reports))
	}
	if resp.Reports[0].TenantID != "tenant-a" {
		t.Errorf("expected tenantID=tenant-a, got %q", resp.Reports[0].TenantID)
	}
}

func TestListReportsHandler_AdminSeesAll(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	store.SaveReports([]reconciliation.Report{
		{SubscriptionID: "sub-1", TenantID: "tenant-a"},
		{SubscriptionID: "sub-2", TenantID: "tenant-b"},
	})

	r := setupReconcileRouter(nil, store, "any-tenant", string(auth.RoleAdmin))
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Reports) != 2 {
		t.Errorf("expected 2 reports, got %d", len(resp.Reports))
	}
}

func TestListReportsHandler_InvalidCursor(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(nil, store, "tenant-1", string(auth.RoleMerchant))

	req := httptest.NewRequest(http.MethodGet, "/reports?cursor=invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid cursor, got %d; body: %s", w.Code, w.Body.String())
	}

	var response ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if response.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %s", response.Code)
	}
}

func TestListReportsHandler_RejectsScopedCursorFromDifferentTenant(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(nil, store, "tenant-b", string(auth.RoleMerchant))

	cursor := pagination.EncodeScopedCursor("sub-1", "sub-1", "tenant-a")
	req := httptest.NewRequest(http.MethodGet, "/reports?cursor="+cursor, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-tenant cursor, got %d; body: %s", w.Code, w.Body.String())
	}

	var response ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if response.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %s", response.Code)
	}
	if response.Details["reason"] != "cursor does not belong to this tenant" {
		t.Fatalf("expected tenant mismatch reason, got %#v", response.Details["reason"])
	}
}

func TestListReportsHandler_LimitClamping(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	// Add 30 reports
	reports := make([]reconciliation.Report, 30)
	for i := 0; i < 30; i++ {
		reports[i] = reconciliation.Report{
			SubscriptionID: fmt.Sprintf("sub-%d", i),
			TenantID:       "tenant-1",
		}
	}
	store.SaveReports(reports)

	r := setupReconcileRouter(nil, store, "tenant-1", string(auth.RoleMerchant))

	// Request with limit 100, should be clamped to 20
	req := httptest.NewRequest(http.MethodGet, "/reports?limit=100", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(resp.Reports) > 20 {
		t.Errorf("expected at most 20 reports due to clamping, got %d", len(resp.Reports))
	}
}

func TestListReportsHandler_InvalidLimit(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(nil, store, "tenant-1", "merchant")

	req := httptest.NewRequest(http.MethodGet, "/admin/reports?limit=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d: %s", w.Code, w.Body.String())
	}

	var response ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if response.Code != "VALIDATION_FAILED" {
		t.Fatalf("expected VALIDATION_FAILED, got %s", response.Code)
	}
}
