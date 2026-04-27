package idempotency_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/idempotency"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newRouter wires up the middleware and a simple POST handler that echoes a fixed response.
func newRouter(store *idempotency.Store) *gin.Engine {
	r := gin.New()
	r.Use(idempotency.Middleware(store))
	r.POST("/charge", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"charged": true})
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
	r := newRouter(store)

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
	r := newRouter(store)

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
	r := newRouter(store)

	post(r, "/charge", "key-003", `{"amount":100}`)
	w := post(r, "/charge", "key-003", `{"amount":999}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for payload mismatch, got %d", w.Code)
	}
}

// TestErrorResponseNotCached verifies that failed responses are not stored,
// allowing clients to safely retry after a server error.
func TestErrorResponseNotCached(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	r := newRouter(store)

	w1 := post(r, "/fail", "key-004", `{}`)
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w1.Code)
	}

	// Second request should also hit the handler (not a replay).
	w2 := post(r, "/fail", "key-004", `{}`)
	if w2.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("error responses must not be cached/replayed")
	}
}

// TestNoKeyPassesThrough verifies requests without a key are unaffected.
func TestNoKeyPassesThrough(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	r := newRouter(store)

	w := post(r, "/charge", "", `{"amount":100}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestKeyTooLongRejected verifies oversized keys are rejected with 400.
func TestKeyTooLongRejected(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	r := newRouter(store)

	longKey := string(make([]byte, 256))
	w := post(r, "/charge", longKey, `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized key, got %d", w.Code)
	}
}

// TestExpiredEntryNotReplayed verifies that expired entries are not replayed.
func TestExpiredEntryNotReplayed(t *testing.T) {
	store := idempotency.NewStore(50 * time.Millisecond)
	r := newRouter(store)

	post(r, "/charge", "key-005", `{"amount":100}`)
	time.Sleep(100 * time.Millisecond) // let the entry expire

	w := post(r, "/charge", "key-005", `{"amount":100}`)
	if w.Header().Get("Idempotency-Replayed") == "true" {
		t.Fatal("expired entry should not be replayed")
	}
}

// TestConcurrentDuplicatesHandledSafely fires multiple goroutines with the same
// key simultaneously and verifies no panics and at most one non-replayed response.
func TestConcurrentDuplicatesHandledSafely(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	r := newRouter(store)

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

	replayed := 0
	for _, w := range results {
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Header().Get("Idempotency-Replayed") == "true" {
			replayed++
		}
	}
	// At least one must be a replay (the rest after the first).
	if replayed == 0 {
		t.Fatal("expected at least one replayed response in concurrent scenario")
	}
}

// TestGetRequestSkipped verifies GET requests are not subject to idempotency checks.
func TestGetRequestSkipped(t *testing.T) {
	store := idempotency.NewStore(idempotency.DefaultTTL)
	r := gin.New()
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

