package idempotency_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/idempotency"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// withCaller mounts a stub auth middleware that injects callerID/tenantID into
// the Gin context, mirroring what middleware.AuthMiddleware does in production.
// Empty values simulate an unauthenticated request.
func withCaller(caller, tenant string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if caller != "" {
			c.Set("callerID", caller)
		}
		if tenant != "" {
			c.Set("tenantID", tenant)
		}
		c.Next()
	}
}

// newRouter wires up the middleware and a simple POST handler that echoes a fixed response.
func newRouter(store *idempotency.Store, caller, tenant string) *gin.Engine {
	r := gin.New()
	r.Use(withCaller(caller, tenant))
	r.Use(idempotency.Middleware(store))
	r.POST("/charge", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"charged": true})
	})
	r.POST("/refund", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"refunded": true})
	})
	r.POST("/fail", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})
	return r
}

func post(r *gin.Engine, path, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestFirstRequestProcessed verifies a normal request goes through.
func TestFirstRequestProcessed(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	w := post(r, "/charge", "key-001", `{"amount":100}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Idempotency-Replayed") != "" {
		t.Fatal("first request should not be marked as replayed")
	}
}

// TestReplayReturnsCachedResponse verifies the second request with the same key
// returns the cached response without hitting the handler again.
func TestReplayReturnsCachedResponse(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	post(r, "/charge", "key-002", `{"amount":100}`)
	w := post(r, "/charge", "key-002", `{"amount":100}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("replayed response should have Idempotency-Replayed: true header")
	}
}

// TestPayloadMismatchRejected verifies that reusing a key with a different body returns 422.
func TestPayloadMismatchRejected(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	post(r, "/charge", "key-003", `{"amount":100}`)
	w := post(r, "/charge", "key-003", `{"amount":999}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for payload mismatch, got %d", w.Code)
	}
}

// TestMethodPathMismatchRejected verifies that reusing a key against a
// different route is rejected. This stops a key issued for /charge from
// being silently replayed against /refund.
func TestMethodPathMismatchRejected(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	post(r, "/charge", "key-path", `{"amount":100}`)
	w := post(r, "/refund", "key-path", `{"amount":100}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for path mismatch, got %d", w.Code)
	}
}

// TestErrorResponseNotCached verifies that failed responses are not stored,
// allowing clients to safely retry after a server error.
func TestErrorResponseNotCached(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	w1 := post(r, "/fail", "key-004", `{}`)
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w1.Code)
	}

	w2 := post(r, "/fail", "key-004", `{}`)
	if w2.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("error responses must not be cached/replayed")
	}
}

// TestNoKeyPassesThrough verifies requests without a key are unaffected.
func TestNoKeyPassesThrough(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	w := post(r, "/charge", "", `{"amount":100}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestKeyTooLongRejected verifies oversized keys are rejected with 400.
func TestKeyTooLongRejected(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	longKey := string(make([]byte, 256))
	w := post(r, "/charge", longKey, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized key, got %d", w.Code)
	}
}

// TestExpiredEntryNotReplayed verifies that expired entries are not replayed.
func TestExpiredEntryNotReplayed(t *testing.T) {
	store := idempotency.NewStore(50 * time.Millisecond)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	post(r, "/charge", "key-005", `{"amount":100}`)
	time.Sleep(120 * time.Millisecond)

	w := post(r, "/charge", "key-005", `{"amount":100}`)
	if w.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("expired entry should not be replayed")
	}
}

// TestConcurrentDuplicatesHandledSafely fires multiple goroutines with the same
// key simultaneously and verifies the handler runs at most once and the rest
// receive a replayed response with identical bodies.
func TestConcurrentDuplicatesHandledSafely(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()

	var handlerHits int64
	r := gin.New()
	r.Use(withCaller("user-a", "tenant-1"))
	r.Use(idempotency.Middleware(store))
	r.POST("/charge", func(c *gin.Context) {
		atomic.AddInt64(&handlerHits, 1)
		// Sleep so the inflight lock window is long enough to overlap.
		time.Sleep(20 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"charged": true})
	})

	const n = 20
	results := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i] = post(r, "/charge", "key-concurrent", `{"amount":100}`)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&handlerHits); got != 1 {
		t.Fatalf("expected handler to run exactly once, got %d hits", got)
	}

	replayed := 0
	for _, w := range results {
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Header().Get("Idempotency-Replayed") == "true" {
			replayed++
		}
	}
	if replayed != n-1 {
		t.Fatalf("expected %d replays, got %d", n-1, replayed)
	}
}

// TestGetRequestSkipped verifies GET requests are not subject to idempotency checks.
func TestGetRequestSkipped(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := gin.New()
	r.Use(withCaller("user-a", "tenant-1"))
	r.Use(idempotency.Middleware(store))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pong": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Idempotency-Key", "key-get")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("GET requests should never be intercepted")
	}
}

// TestCrossUserKeyIsolation verifies two different callers using the same
// Idempotency-Key value do not collide. This is the core security property:
// caller B must never receive caller A's cached response.
func TestCrossUserKeyIsolation(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()

	rA := newRouter(store, "user-a", "tenant-1")
	rB := newRouter(store, "user-b", "tenant-1")

	wA := post(rA, "/charge", "shared-key", `{"amount":100}`)
	if wA.Code != http.StatusOK || wA.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatalf("user-a first request unexpected: code=%d replayed=%q", wA.Code, wA.Header().Get("Idempotency-Replayed"))
	}

	// Same key, different user, intentionally a different payload — must be
	// processed fresh, not rejected as a payload mismatch and not replayed
	// from user-a's cached entry.
	wB := post(rB, "/charge", "shared-key", `{"amount":250}`)
	if wB.Code != http.StatusOK {
		t.Fatalf("user-b request must succeed, got %d", wB.Code)
	}
	if wB.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("user-b must not receive user-a's cached response")
	}
}

// TestCrossTenantKeyIsolation verifies the scope also splits across tenants.
func TestCrossTenantKeyIsolation(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()

	rA := newRouter(store, "user-shared", "tenant-1")
	rB := newRouter(store, "user-shared", "tenant-2")

	post(rA, "/charge", "tenant-key", `{"amount":100}`)
	wB := post(rB, "/charge", "tenant-key", `{"amount":100}`)
	if wB.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("tenant-2 must not receive tenant-1's cached response")
	}
}

// TestSameUserReplayWithinScope verifies that within a single scope, the
// expected replay behavior still works.
func TestSameUserReplayWithinScope(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	defer store.Stop()
	r := newRouter(store, "user-a", "tenant-1")

	post(r, "/charge", "scoped-key", `{"amount":100}`)
	w := post(r, "/charge", "scoped-key", `{"amount":100}`)

	if w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("same-user replay must be marked as replayed")
	}
}

// TestStoreStopIsIdempotent verifies Stop can be called multiple times safely.
func TestStoreStopIsIdempotent(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	store.Stop()
	store.Stop() // must not panic
}

// TestHashPayloadStable confirms the payload hash is deterministic — required
// for replay matching across processes.
func TestHashPayloadStable(t *testing.T) {
	a := idempotency.HashPayload([]byte(`{"amount":100}`))
	b := idempotency.HashPayload([]byte(`{"amount":100}`))
	if a != b {
		t.Fatalf("hash unstable: %s vs %s", a, b)
	}
	if a == idempotency.HashPayload([]byte(`{"amount":101}`)) {
		t.Fatal("hashes must differ for different payloads")
	}
}
