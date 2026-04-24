package handlers

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/reconciliation"
)

func TestReconcileHandler(t *testing.T) {
    now := time.Now().UTC()

    // prepare adapter with a snapshot that will mismatch status and balances
    snap := reconciliation.Snapshot{
        SubscriptionID: "550e8400-e29b-41d4-a716-446655440000",
        Status: "cancelled",
        Amount: 1000,
        Currency: "USD",
        Interval: "monthly",
        Balances: map[string]int64{"due": 0},
        ExportedAt: now,
    }
    adapter := reconciliation.NewMemoryAdapter(snap)

    // backend submission with differing status and due balance
    backend := reconciliation.BackendSubscription{
        SubscriptionID: "550e8400-e29b-41d4-a716-446655440000",
        Status: "active",
        Amount: 1000,
        Currency: "USD",
        Interval: "monthly",
        Balances: map[string]int64{"due": 100},
        UpdatedAt: now,
    }

    payload, _ := json.Marshal([]reconciliation.BackendSubscription{backend})

    store := reconciliation.NewMemoryStore()
    r := gin.New()
    r.POST("/admin/reconcile", NewReconcileHandler(adapter, store))

    req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", bytes.NewReader(payload))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
    }

    var resp struct {
        Summary struct{
            Total int `json:"total"`
            Matched int `json:"matched"`
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

    // ensure reports were persisted
    saved, err := store.ListReports()
    if err != nil {
        t.Fatalf("store.ListReports error: %v", err)
    }
    if len(saved) != 1 || saved[0].SubscriptionID != "550e8400-e29b-41d4-a716-446655440000" {
        t.Fatalf("unexpected saved reports: %#v", saved)
    }
}
