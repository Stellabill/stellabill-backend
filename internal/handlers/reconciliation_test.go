package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/reconciliation"
)

func setupReconcileRouter(adapter reconciliation.Adapter, store reconciliation.Store, tenantID, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("callerID", "caller-123")
		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		if role != "" {
			c.Set(auth.RolesContextKey, []auth.Role{auth.Role(role)})
		}
		c.Next()
	})
	r.POST("/admin/reconcile", NewReconcileHandler(adapter, store))
	r.GET("/admin/reports", NewListReportsHandler(store))
	return r
}

func TestReconcileHandler(t *testing.T) {
	now := time.Now().UTC()

	snap := reconciliation.Snapshot{
		SubscriptionID: "sub-1",
		TenantID:       "tenant-1",
		Status:         "cancelled",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{"due": 0},
		ExportedAt:     now,
	}
	adapter := reconciliation.NewMemoryAdapter(snap)

	backend := reconciliation.BackendSubscription{
		SubscriptionID: "sub-1",
		TenantID:       "tenant-1",
		Status:         "active",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{"due": 100},
		UpdatedAt:      now,
	}

	payload, _ := json.Marshal([]reconciliation.BackendSubscription{backend})

	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(adapter, store, "tenant-1", "admin")

	req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summary struct {
			Total      int `json:"total"`
			Matched    int `json:"matched"`
			Mismatched int `json:"mismatched"`
		} `json:"summary"`
		Reports []reconciliation.Report `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Summary.Total != 1 || resp.Summary.Mismatched != 1 || len(resp.Reports) != 1 {
		t.Fatalf("unexpected summary/reports: %+v", resp)
	}
	if resp.Reports[0].Matched {
		t.Fatalf("expected report to show mismatches")
	}

	saved, err := store.ListReports()
	if err != nil {
		t.Fatalf("store.ListReports error: %v", err)
	}
	if len(saved) != 1 || saved[0].SubscriptionID != "sub-1" {
		t.Fatalf("unexpected saved reports: %#v", saved)
	}
}

func TestReconcileHandler_NoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// No callerID set
	r.POST("/admin/reconcile", NewReconcileHandler(reconciliation.NewMemoryAdapter(), reconciliation.NewMemoryStore()))

	req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader([]byte("[]")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestReconcileHandler_CustomerForbidden(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(reconciliation.NewMemoryAdapter(), store, "tenant-1", "customer")

	req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader([]byte("[]")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReconcileHandler_MerchantCrossTenantBlocked(t *testing.T) {
	adapter := reconciliation.NewMemoryAdapter()
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(adapter, store, "tenant-1", "merchant")

	backend := reconciliation.BackendSubscription{
		SubscriptionID: "sub-other",
		TenantID:       "tenant-2",
		Status:         "active",
		Amount:         500,
		Currency:       "USD",
		Interval:       "monthly",
		UpdatedAt:      time.Now().UTC(),
	}
	payload, _ := json.Marshal([]reconciliation.BackendSubscription{backend})

	req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant attempt, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReconcileHandler_MerchantScopedSnapshots(t *testing.T) {
	now := time.Now().UTC()

	snap1 := reconciliation.Snapshot{
		SubscriptionID: "sub-mine",
		TenantID:       "tenant-1",
		Status:         "active",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		ExportedAt:     now,
	}
	snap2 := reconciliation.Snapshot{
		SubscriptionID: "sub-other",
		TenantID:       "tenant-2",
		Status:         "active",
		Amount:         2000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		ExportedAt:     now,
	}
	adapter := reconciliation.NewMemoryAdapter(snap1, snap2)
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(adapter, store, "tenant-1", "merchant")

	backend := reconciliation.BackendSubscription{
		SubscriptionID: "sub-mine",
		TenantID:       "tenant-1",
		Status:         "active",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		UpdatedAt:      now,
	}
	payload, _ := json.Marshal([]reconciliation.BackendSubscription{backend})

	req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(resp.Reports))
	}
	if resp.Reports[0].TenantID != "tenant-1" {
		t.Fatalf("report tenant should be tenant-1, got %s", resp.Reports[0].TenantID)
	}
}

func TestListReportsHandler_TenantIsolation(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	_ = store.SaveReports([]reconciliation.Report{
		{SubscriptionID: "sub-1", TenantID: "tenant-1", Matched: true},
		{SubscriptionID: "sub-2", TenantID: "tenant-2", Matched: false},
		{SubscriptionID: "sub-3", TenantID: "tenant-1", Matched: true},
	})

	// Merchant for tenant-1 should only see their reports
	r := setupReconcileRouter(nil, store, "tenant-1", "merchant")

	req := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Reports) != 2 {
		t.Fatalf("expected 2 tenant-1 reports, got %d", len(resp.Reports))
	}
	for _, rpt := range resp.Reports {
		if rpt.TenantID != "tenant-1" {
			t.Fatalf("leaked report from tenant %s to tenant-1", rpt.TenantID)
		}
	}
}

func TestListReportsHandler_AdminSeesAll(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	_ = store.SaveReports([]reconciliation.Report{
		{SubscriptionID: "sub-1", TenantID: "tenant-1", Matched: true},
		{SubscriptionID: "sub-2", TenantID: "tenant-2", Matched: false},
	})

	r := setupReconcileRouter(nil, store, "tenant-1", "admin")

	req := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Reports []reconciliation.Report `json:"reports"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Reports) != 2 {
		t.Fatalf("admin should see all 2 reports, got %d", len(resp.Reports))
	}
}

func TestListReportsHandler_CustomerForbidden(t *testing.T) {
	r := setupReconcileRouter(nil, reconciliation.NewMemoryStore(), "tenant-1", "customer")

	req := httptest.NewRequest(http.MethodGet, "/admin/reports", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for customer, got %d", w.Code)
	}
}

func TestListReportsHandler_CrossTenantCursorRejected(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	_ = store.SaveReports([]reconciliation.Report{
		{SubscriptionID: "sub-1", TenantID: "tenant-1", Matched: true},
	})

	// Create a cursor scoped to tenant-2
	cursorForTenant2 := pagination.EncodeScopedCursor("sub-1", "sub-1", "tenant-2")

	r := setupReconcileRouter(nil, store, "tenant-1", "merchant")

	req := httptest.NewRequest(http.MethodGet, "/admin/reports?cursor="+cursorForTenant2, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-tenant cursor, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListReportsHandler_TamperedCursorRejected(t *testing.T) {
	store := reconciliation.NewMemoryStore()
	r := setupReconcileRouter(nil, store, "tenant-1", "merchant")

	// Hand-craft a tampered cursor (not properly signed)
	req := httptest.NewRequest(http.MethodGet, "/admin/reports?cursor=dGFtcGVyZWQ=", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for tampered cursor, got %d: %s", w.Code, w.Body.String())
	}
}
