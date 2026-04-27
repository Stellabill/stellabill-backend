package reconciliation

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "go.uber.org/zap"
)

func TestHTTPAdapter_FetchSnapshots(t *testing.T) {
    now := time.Now().UTC()
    snaps := []Snapshot{
        {
            SubscriptionID: "sub-http-1",
            Status:         "active",
            Amount:         1200,
            Currency:       "USD",
            Interval:       "monthly",
            Balances:       map[string]int64{"due": 0},
            ExportedAt:     now,
        },
    }

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(snaps)
    }))
    defer srv.Close()

    adapter := NewHTTPAdapter(srv.URL, "", zap.NewNop())
    got, err := adapter.FetchSnapshots(context.Background())
    if err != nil {
        t.Fatalf("FetchSnapshots error: %v", err)
    }
    if len(got) != 1 || got[0].SubscriptionID != "sub-http-1" {
        t.Fatalf("unexpected snapshots: %#v", got)
    }
}
