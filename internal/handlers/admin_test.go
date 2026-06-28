package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/repository"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// mockPurgeable is a test double for cache.Purgeable.
type mockPurgeable struct {
	mu           sync.Mutex
	namespace    string
	keysToReturn int
	flushErr     error
	flushCalls   int
	resetCalls   int
}

func newMockPurgeable(ns string, keys int) *mockPurgeable {
	return &mockPurgeable{namespace: ns, keysToReturn: keys}
}

func newErrPurgeable(ns string, err error) *mockPurgeable {
	return &mockPurgeable{namespace: ns, flushErr: err}
}

func (m *mockPurgeable) Flush(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushCalls++
	if m.flushErr != nil {
		return 0, m.flushErr
	}
	n := m.keysToReturn
	m.keysToReturn = 0 // second call returns 0 (idempotent)
	return n, nil
}

func (m *mockPurgeable) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetCalls++
}

func (m *mockPurgeable) Namespace() string { return m.namespace }

// buildRouter wires an audit logger + admin handler into a Gin router.
func buildRouter(sink *audit.MemorySink, handler *AdminHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	logger := audit.NewLogger("secret", sink)
	r.Use(audit.Middleware(logger))
	r.POST("/api/admin/purge", handler.PurgeCache)
	return r
}

func doRequest(r *gin.Engine, token, adminUser, extraQuery string) *httptest.ResponseRecorder {
	url := "/api/admin/purge"
	if extraQuery != "" {
		url += "?" + extraQuery
	}
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	if adminUser != "" {
		req.Header.Set("X-Admin-User", adminUser)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodePurgeResponse(t *testing.T, rec *httptest.ResponseRecorder) purgeResponse {
	t.Helper()
	var resp purgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode purgeResponse: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

func lastEntry(sink *audit.MemorySink) audit.AuditEvent {
	entries := sink.Entries()
	if len(entries) == 0 {
		return audit.AuditEvent{}
	}
	return entries[len(entries)-1]
}

// ── backward-compatible tests (original behaviour preserved) ─────────────────

func TestAdminPurgeSuccess(t *testing.T) {
	sink := &audit.MemorySink{}
	handler := NewAdminHandler("token")
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "root", "target=cache&attempt=2")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entry := lastEntry(sink)
	if entry.Outcome != "success" {
		t.Fatalf("expected audit outcome 'success', got %q", entry.Outcome)
	}
	if entry.Resource != "cache" {
		t.Fatalf("expected audit target 'cache', got %q", entry.Resource)
	}
	if entry.Metadata["attempt"] != "2" {
		t.Fatalf("expected attempt metadata '2', got %q", entry.Metadata["attempt"])
	}
}

func TestAdminPurgePartialAndRetry(t *testing.T) {
	sink := &audit.MemorySink{}
	handler := NewAdminHandler("token")
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "partial=1&attempt=3")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	entry := lastEntry(sink)
	if entry.Outcome != "partial" {
		t.Fatalf("expected audit outcome 'partial', got %q", entry.Outcome)
	}
	if entry.Metadata["attempt"] != "3" {
		t.Fatalf("expected attempt metadata '3', got %q", entry.Metadata["attempt"])
	}
}

func TestAdminPurgeDenied(t *testing.T) {
	sink := &audit.MemorySink{}
	handler := NewAdminHandler("token")
	r := buildRouter(sink, handler)

	rec := doRequest(r, "wrong", "", "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// The very first audit entry should be the denied purge action.
	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	first := entries[0]
	if first.Action != "admin_purge" {
		t.Fatalf("expected action 'admin_purge', got %q", first.Action)
	}
	if first.Outcome != "denied" {
		t.Fatalf("expected outcome 'denied', got %q", first.Outcome)
	}
}

func TestAdminDefaultToken(t *testing.T) {
	sink := &audit.MemorySink{}
	handler := NewAdminHandler("")
	r := buildRouter(sink, handler)

	rec := doRequest(r, "change-me-admin-token", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := decodePurgeResponse(t, rec)
	if resp.Status != "purged" {
		t.Fatalf("expected status 'purged', got %q", resp.Status)
	}

	entry := lastEntry(sink)
	if entry.Action != "admin_purge" || entry.Outcome != "success" {
		t.Fatalf("unexpected audit entry: action=%q outcome=%q", entry.Action, entry.Outcome)
	}
}


// ── new tests: real cache invalidation behaviour ─────────────────────────────

func TestAdminPurge_FullPurge(t *testing.T) {
	sink := &audit.MemorySink{}
	plans := newMockPurgeable("plans", 4)
	subs := newMockPurgeable("subscriptions", 7)
	handler := NewAdminHandler("token", plans, subs)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "admin", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	resp := decodePurgeResponse(t, rec)
	if resp.TotalKeysPurged != 11 {
		t.Fatalf("expected 11 total keys purged, got %d", resp.TotalKeysPurged)
	}
	if len(resp.Namespaces) != 2 {
		t.Fatalf("expected 2 namespaces, got %d", len(resp.Namespaces))
	}
	if resp.Status != "purged" {
		t.Fatalf("expected status 'purged', got %q", resp.Status)
	}
	for _, ns := range resp.Namespaces {
		if !ns.CountersReset {
			t.Errorf("namespace %q: counters_reset should be true", ns.Namespace)
		}
		if ns.Error != "" {
			t.Errorf("namespace %q: unexpected error %q", ns.Namespace, ns.Error)
		}
	}
	if resp.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp in response")
	}

	// Verify audit entry
	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("expected audit entry")
	}
	last := entries[len(entries)-1]
	if last.Outcome != "success" {
		t.Fatalf("expected audit outcome 'success', got %q", last.Outcome)
	}
	if last.Metadata["keys_purged"] != "11" {
		t.Fatalf("expected keys_purged=11 in audit, got %q", last.Metadata["keys_purged"])
	}
}

func TestAdminPurge_EmptyCache(t *testing.T) {
	sink := &audit.MemorySink{}
	plans := newMockPurgeable("plans", 0)
	subs := newMockPurgeable("subscriptions", 0)
	handler := NewAdminHandler("token", plans, subs)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on empty cache, got %d", rec.Code)
	}
	resp := decodePurgeResponse(t, rec)
	if resp.TotalKeysPurged != 0 {
		t.Fatalf("expected 0 keys purged on empty cache, got %d", resp.TotalKeysPurged)
	}
	if resp.Status != "purged" {
		t.Fatalf("expected status 'purged', got %q", resp.Status)
	}
}

func TestAdminPurge_RepeatedPurge_Idempotent(t *testing.T) {
	sink := &audit.MemorySink{}
	plans := newMockPurgeable("plans", 5)
	handler := NewAdminHandler("token", plans)
	r := buildRouter(sink, handler)

	// First call — should purge 5 keys
	rec1 := doRequest(r, "token", "", "")
	if rec1.Code != http.StatusOK {
		t.Fatalf("first purge: expected 200, got %d", rec1.Code)
	}
	resp1 := decodePurgeResponse(t, rec1)
	if resp1.TotalKeysPurged != 5 {
		t.Fatalf("first purge: expected 5, got %d", resp1.TotalKeysPurged)
	}

	// Second call — cache is already empty, should return 0 without error
	rec2 := doRequest(r, "token", "", "")
	if rec2.Code != http.StatusOK {
		t.Fatalf("second purge: expected 200, got %d", rec2.Code)
	}
	resp2 := decodePurgeResponse(t, rec2)
	if resp2.TotalKeysPurged != 0 {
		t.Fatalf("second purge: expected 0, got %d", resp2.TotalKeysPurged)
	}
	if resp2.Status != "purged" {
		t.Fatalf("second purge: expected status 'purged', got %q", resp2.Status)
	}
}

func TestAdminPurge_CounterReset(t *testing.T) {
	sink := &audit.MemorySink{}
	p := newMockPurgeable("plans", 3)
	handler := NewAdminHandler("token", p)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	p.mu.Lock()
	rc := p.resetCalls
	p.mu.Unlock()

	if rc != 1 {
		t.Fatalf("expected ResetMetrics called once, got %d", rc)
	}

	resp := decodePurgeResponse(t, rec)
	for _, ns := range resp.Namespaces {
		if !ns.CountersReset {
			t.Errorf("namespace %q: counters_reset should be true", ns.Namespace)
		}
	}
}

func TestAdminPurge_CounterResetOnError(t *testing.T) {
	// Metrics should be reset even when Flush returns an error.
	sink := &audit.MemorySink{}
	p := newErrPurgeable("subscriptions", errors.New("redis unavailable"))
	handler := NewAdminHandler("token", p)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on flush error, got %d", rec.Code)
	}

	p.mu.Lock()
	rc := p.resetCalls
	p.mu.Unlock()

	if rc != 1 {
		t.Fatalf("ResetMetrics should be called even on Flush error, got %d calls", rc)
	}
}

func TestAdminPurge_PartialFailure(t *testing.T) {
	sink := &audit.MemorySink{}
	good := newMockPurgeable("plans", 3)
	bad := newErrPurgeable("subscriptions", errors.New("cache unavailable"))
	handler := NewAdminHandler("token", good, bad)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on partial failure, got %d", rec.Code)
	}
	resp := decodePurgeResponse(t, rec)
	if resp.Status != "partial" {
		t.Fatalf("expected status 'partial', got %q", resp.Status)
	}

	nsMap := make(map[string]namespaceSummary)
	for _, ns := range resp.Namespaces {
		nsMap[ns.Namespace] = ns
	}
	if nsMap["plans"].KeysPurged != 3 {
		t.Fatalf("plans: expected 3 keys purged, got %d", nsMap["plans"].KeysPurged)
	}
	if nsMap["subscriptions"].Error == "" {
		t.Fatal("subscriptions: expected error in summary, got none")
	}

	// Audit outcome should be "partial"
	entries := sink.Entries()
	last := entries[len(entries)-1]
	if last.Outcome != "partial" {
		t.Fatalf("audit outcome: expected 'partial', got %q", last.Outcome)
	}
}

func TestAdminPurge_NoPurgeables(t *testing.T) {
	// Handler with no purgeables must still succeed (zero namespaces).
	sink := &audit.MemorySink{}
	handler := NewAdminHandler("token") // no purgeables
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := decodePurgeResponse(t, rec)
	if resp.TotalKeysPurged != 0 {
		t.Fatalf("expected 0 keys purged, got %d", resp.TotalKeysPurged)
	}
	if len(resp.Namespaces) != 0 {
		t.Fatalf("expected empty namespaces slice, got %d", len(resp.Namespaces))
	}
}

func TestAdminPurge_Concurrent(t *testing.T) {
	// Multiple goroutines purging simultaneously must not race or panic.
	sink := &audit.MemorySink{}
	plans := newMockPurgeable("plans", 100)
	subs := newMockPurgeable("subscriptions", 200)
	handler := NewAdminHandler("token", plans, subs)
	r := buildRouter(sink, handler)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRequest(r, "token", "", "")
			if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
				t.Errorf("concurrent purge: unexpected status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

// ── real repo integration tests (no mocks, real InMemory cache) ──────────────

func TestAdminPurge_WithRealRepos(t *testing.T) {
	ctx := context.Background()

	planCache := cache.NewInMemory()
	subCache := cache.NewInMemory()

	planBackend := repository.NewMockPlanRepo(
		&repository.PlanRow{ID: "p1", Name: "Basic", Amount: "999", Currency: "usd", Interval: "month"},
		&repository.PlanRow{ID: "p2", Name: "Pro", Amount: "1999", Currency: "usd", Interval: "month"},
	)
	subBackend := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "s1", Status: "active", Amount: "999", Currency: "usd", Interval: "month"},
	)

	cachedPlans := repository.NewCachedPlanRepo(planBackend, planCache, 0)
	cachedSubs := repository.NewCachedSubscriptionRepo(subBackend, subCache, 0)

	// Populate the caches with a few reads
	_, _ = cachedPlans.FindByID(ctx, "p1")
	_, _ = cachedPlans.FindByID(ctx, "p2")
	_, _ = cachedPlans.List(ctx)
	_, _ = cachedSubs.FindByID(ctx, "s1")

	if planCache.Len() == 0 {
		t.Fatal("expected plan cache to have entries before purge")
	}
	if subCache.Len() == 0 {
		t.Fatal("expected subscription cache to have entries before purge")
	}

	// Verify hits accumulated
	planHits, _, _ := cachedPlans.Metrics()
	// p1 and p2 listed via List; plan:list:all should exist.
	// Second FindByID after List would be a cache hit — but we only called once.
	// Misses should be non-zero regardless.
	_, planMisses, _ := cachedPlans.Metrics()
	if planMisses == 0 && planHits == 0 {
		t.Fatal("expected non-zero metrics before purge")
	}

	sink := &audit.MemorySink{}
	handler := NewAdminHandler("token", cachedPlans, cachedSubs)
	r := buildRouter(sink, handler)

	rec := doRequest(r, "token", "ops-team", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rec.Code, rec.Body.String())
	}

	resp := decodePurgeResponse(t, rec)
	if resp.TotalKeysPurged == 0 {
		t.Fatalf("expected non-zero total_keys_purged, got 0")
	}

	// Caches must be empty after purge
	if planCache.Len() != 0 {
		t.Fatalf("plan cache not empty after purge: %d entries remain", planCache.Len())
	}
	if subCache.Len() != 0 {
		t.Fatalf("subscription cache not empty after purge: %d entries remain", subCache.Len())
	}

	// Metrics must have been reset
	h2, m2, _ := cachedPlans.Metrics()
	if h2 != 0 || m2 != 0 {
		t.Fatalf("plan metrics not reset: hits=%d misses=%d", h2, m2)
	}
	h3, m3 := cachedSubs.Metrics()
	if h3 != 0 || m3 != 0 {
		t.Fatalf("sub metrics not reset: hits=%d misses=%d", h3, m3)
	}

	// Subsequent reads re-populate from backend (no stale data)
	p1, err := cachedPlans.FindByID(ctx, "p1")
	if err != nil || p1.ID != "p1" {
		t.Fatalf("post-purge FindByID: %v %v", p1, err)
	}
}

