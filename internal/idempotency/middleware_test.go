package idempotency_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/testutil"
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

func TestDBMiddleware(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	var container *testutil.ContainerDSN
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic starting postgres container: %v", r)
			}
		}()
		container, err = testutil.StartPostgresContainer(ctx)
	}()
	if err != nil {
		t.Skipf("skipping TestDBMiddleware: postgres container could not be started: %v", err)
	}
	defer func() {
		if err := container.Teardown(ctx); err != nil {
			t.Logf("error tearing down container: %v", err)
		}
	}()

	err = testutil.ApplyMigrations(ctx, container.DSN)
	if err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	pool, err := testutil.NewPoolFromDSN(ctx, container.DSN)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	// Create DBStore with a short TTL (100ms) for testing expiration
	dbStore := idempotency.NewDBStore(pool, 200*time.Millisecond)

	// Setup a test router
	r := gin.New()
	r.Use(idempotency.DBMiddleware(dbStore))

	r.POST("/admin/action", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.Header("X-Custom-Response", "custom-header-val")
		c.Data(http.StatusOK, "application/json", body)
	})

	r.POST("/admin/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal-server-error"})
	})

	r.POST("/admin/panic", func(c *gin.Context) {
		panic("forced panic")
	})

	postWithHeaders := func(path, key string, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 1. Missing Idempotency-Key header -> rejected with 400
	t.Run("missing key", func(t *testing.T) {
		w := postWithHeaders("/admin/action", "", `{"data":1}`, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	// 2. Oversized key -> rejected with 400
	t.Run("oversized key", func(t *testing.T) {
		longKey := strings.Repeat("a", 256)
		w := postWithHeaders("/admin/action", longKey, `{"data":1}`, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	// 3. Successful first request persistence and replay
	t.Run("first request and replay", func(t *testing.T) {
		key := "test-key-1"
		headers := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}

		w1 := postWithHeaders("/admin/action", key, `{"data":1}`, headers)
		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w1.Code)
		}
		if w1.Header().Get("Idempotency-Replayed") == "true" {
			t.Error("first request should not be marked as replayed")
		}
		if w1.Header().Get("X-Custom-Response") != "custom-header-val" {
			t.Errorf("missing custom header, got %s", w1.Header().Get("X-Custom-Response"))
		}

		// Replay
		w2 := postWithHeaders("/admin/action", key, `{"data":1}`, headers)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 on replay, got %d", w2.Code)
		}
		if w2.Header().Get("Idempotency-Replayed") != "true" {
			t.Error("replayed request should have Idempotency-Replayed: true")
		}
		if w2.Header().Get("X-Custom-Response") != "custom-header-val" {
			t.Errorf("missing custom header on replay, got %s", w2.Header().Get("X-Custom-Response"))
		}
		if w2.Body.String() != `{"data":1}` {
			t.Errorf("expected body %s, got %s", `{"data":1}`, w2.Body.String())
		}
	})

	// 4. Payload mismatch rejected with 422
	t.Run("payload mismatch", func(t *testing.T) {
		key := "test-key-2"
		headers := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}
		postWithHeaders("/admin/action", key, `{"data":1}`, headers)

		w := postWithHeaders("/admin/action", key, `{"data":2}`, headers)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 for payload mismatch, got %d", w.Code)
		}
	})

	// 5. Tenant/user scoping isolation
	t.Run("scoping isolation", func(t *testing.T) {
		key := "test-key-3"
		headers1 := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}
		headers2 := map[string]string{
			"X-Tenant-ID":  "tenant-2",
			"X-Admin-User": "admin-1",
		}
		headers3 := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-2",
		}

		w1 := postWithHeaders("/admin/action", key, `{"data":1}`, headers1)
		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w1.Code)
		}

		// Different tenant with same key should succeed (not replay)
		w2 := postWithHeaders("/admin/action", key, `{"data":1}`, headers2)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 for tenant-2, got %d", w2.Code)
		}
		if w2.Header().Get("Idempotency-Replayed") == "true" {
			t.Error("should not replay across tenants")
		}

		// Different admin user with same key should succeed (not replay)
		w3 := postWithHeaders("/admin/action", key, `{"data":1}`, headers3)
		if w3.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin-2, got %d", w3.Code)
		}
		if w3.Header().Get("Idempotency-Replayed") == "true" {
			t.Error("should not replay across admin users")
		}
	})

	// 6. Concurrent duplicate conflict (409)
	t.Run("concurrent duplicate", func(t *testing.T) {
		key := "test-key-concurrent"
		headers := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}

		chSignal := make(chan struct{})
		chContinue := make(chan struct{})
		r.POST("/admin/slow", func(c *gin.Context) {
			close(chSignal)
			<-chContinue
			c.JSON(http.StatusOK, gin.H{"slow": true})
		})

		var wg sync.WaitGroup
		wg.Add(2)

		var w1, w2 *httptest.ResponseRecorder

		go func() {
			defer wg.Done()
			w1 = postWithHeaders("/admin/slow", key, `{}`, headers)
		}()

		<-chSignal

		go func() {
			defer wg.Done()
			w2 = postWithHeaders("/admin/slow", key, `{}`, headers)
		}()

		close(chContinue)
		wg.Wait()

		if w1.Code != http.StatusOK {
			t.Errorf("expected first request to succeed, got %d", w1.Code)
		}

		if w2.Code != http.StatusConflict {
			t.Errorf("expected concurrent request to get 409, got %d", w2.Code)
		}
	})

	// 7. Expired key behavior
	t.Run("expired key", func(t *testing.T) {
		key := "test-key-expired"
		headers := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}
		w1 := postWithHeaders("/admin/action", key, `{"data":1}`, headers)
		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w1.Code)
		}

		// Wait for TTL (200ms) to expire
		time.Sleep(300 * time.Millisecond)

		w2 := postWithHeaders("/admin/action", key, `{"data":1}`, headers)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w2.Code)
		}
		if w2.Header().Get("Idempotency-Replayed") == "true" {
			t.Error("expired key should not be replayed")
		}
	})

	// 8. Key deleted on error/panic to allow retries
	t.Run("retries on error", func(t *testing.T) {
		key := "test-key-error"
		headers := map[string]string{
			"X-Tenant-ID":  "tenant-1",
			"X-Admin-User": "admin-1",
		}

		w1 := postWithHeaders("/admin/error", key, `{}`, headers)
		if w1.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w1.Code)
		}

		w2 := postWithHeaders("/admin/action", key, `{"data":1}`, headers)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w2.Code)
		}
		if w2.Header().Get("Idempotency-Replayed") == "true" {
			t.Error("should not replay after a previous failure")
		}
	})
}

type mockRow struct {
	scanFunc func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	return r.scanFunc(dest...)
}

type mockPgxConn struct {
	execFunc     func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *mockPgxConn) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return m.execFunc(ctx, sql, arguments...)
}

func (m *mockPgxConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return m.queryRowFunc(ctx, sql, args...)
}

func TestDBMiddlewareMocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A helper to construct the router with DBMiddleware
	setupRouter := func(conn *mockPgxConn) *gin.Engine {
		dbStore := idempotency.NewDBStore(conn, 1*time.Hour)
		r := gin.New()
		r.Use(idempotency.DBMiddleware(dbStore))
		r.POST("/admin/action", func(c *gin.Context) {
			c.Header("X-Response-Header", "hello")
			c.JSON(http.StatusOK, gin.H{"success": true})
		})
		r.POST("/admin/fail", func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "fail"})
		})
		return r
	}

	postRequest := func(r *gin.Engine, key string, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/action", bytes.NewBufferString(body))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("missing key", func(t *testing.T) {
		r := setupRouter(nil)
		w := postRequest(r, "", `{}`, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("oversized key", func(t *testing.T) {
		r := setupRouter(nil)
		w := postRequest(r, strings.Repeat("a", 256), `{}`, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("first request success", func(t *testing.T) {
		execCalls := 0
		var savedBody []byte
		var savedCode int

		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				execCalls++
				if strings.Contains(sql, "INSERT") {
					// Simulate successful acquisition of in_flight key (1 row affected)
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				}
				if strings.Contains(sql, "UPDATE") {
					// SaveResponse call
					savedCode = arguments[2].(int)
					savedBody = arguments[3].([]byte)
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}

		r := setupRouter(conn)
		w := postRequest(r, "key-1", `{"amount":10}`, nil)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if execCalls < 2 {
			t.Errorf("expected store calls, got %d", execCalls)
		}
		if savedCode != http.StatusOK {
			t.Errorf("expected stored code 200, got %d", savedCode)
		}
		if !strings.Contains(string(savedBody), `"success":true`) {
			t.Errorf("expected stored body to match, got %s", string(savedBody))
		}
	})

	t.Run("concurrent in flight duplicate", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					// Return 0 rows affected to simulate ON CONFLICT DO NOTHING
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						// Scan columns: status, response_code, response_body, response_headers, payload_hash
						// status
						*(dest[0].(*string)) = "in_flight"
						return nil
					},
				}
			},
		}

		r := setupRouter(conn)
		w := postRequest(r, "key-concurrent", `{"amount":10}`, nil)

		if w.Code != http.StatusConflict {
			t.Errorf("expected 409 conflict, got %d", w.Code)
		}
	})

	t.Run("payload mismatch", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						// status completed, different payload hash
						*(dest[0].(*string)) = "completed"
						*(dest[4].(*string)) = "different-hash"
						return nil
					},
				}
			},
		}

		r := setupRouter(conn)
		w := postRequest(r, "key-mismatch", `{"amount":10}`, nil)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422 mismatch, got %d", w.Code)
		}
	})

	t.Run("completed replay", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						// status: completed
						*(dest[0].(*string)) = "completed"
						// response_code
						code := http.StatusOK
						*(dest[1].(**int)) = &code
						// response_body
						*(dest[2].(*[]byte)) = []byte(`{"replayed":true}`)
						// response_headers
						*(dest[3].(*[]byte)) = []byte(`{"X-Response-Header":["hello"]}`)
						// payload_hash (same)
						reqBody := `{"amount":10}`
						*(dest[4].(*string)) = idempotency.HashPayload([]byte(reqBody))
						return nil
					},
				}
			},
		}

		r := setupRouter(conn)
		w := postRequest(r, "key-replay", `{"amount":10}`, nil)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if w.Header().Get("Idempotency-Replayed") != "true" {
			t.Errorf("expected replayed header, got %s", w.Header().Get("Idempotency-Replayed"))
		}
		if w.Header().Get("X-Response-Header") != "hello" {
			t.Errorf("expected custom header hello, got %s", w.Header().Get("X-Response-Header"))
		}
		if !strings.Contains(w.Body.String(), `"replayed":true`) {
			t.Errorf("expected replayed body, got %s", w.Body.String())
		}
	})

	t.Run("first request error not cached", func(t *testing.T) {
		deleteCalls := 0
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				}
				if strings.Contains(sql, "DELETE") {
					if !strings.Contains(sql, "expires_at") {
						deleteCalls++
					}
					return pgconn.NewCommandTag("DELETE 1"), nil
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		r := setupRouter(conn)
		req := httptest.NewRequest(http.MethodPost, "/admin/fail", bytes.NewBufferString(`{}`))
		req.Header.Set("Idempotency-Key", "key-fail")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if deleteCalls != 1 {
			t.Errorf("expected 1 delete call, got %d", deleteCalls)
		}
	})

	t.Run("acquire delete expired error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "DELETE") && strings.Contains(sql, "expires_at") {
					return pgconn.CommandTag{}, fmt.Errorf("db error")
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-error", `{}`, nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})

	t.Run("acquire insert error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.CommandTag{}, fmt.Errorf("db error")
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-error", `{}`, nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})

	t.Run("acquire query error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						return fmt.Errorf("db query error")
					},
				}
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-error", `{}`, nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})

	t.Run("acquire query no rows retry success", func(t *testing.T) {
		queryCalls := 0
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				queryCalls++
				return &mockRow{
					scanFunc: func(dest ...any) error {
						if queryCalls == 1 {
							return pgx.ErrNoRows
						}
						// second call success
						*(dest[0].(*string)) = "completed"
						code := http.StatusOK
						*(dest[1].(**int)) = &code
						*(dest[2].(*[]byte)) = []byte(`{"success":true}`)
						*(dest[4].(*string)) = idempotency.HashPayload([]byte(`{}`))
						return nil
					},
				}
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-retry", `{}`, nil)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("completed unmarshal headers error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 0"), nil
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &mockRow{
					scanFunc: func(dest ...any) error {
						*(dest[0].(*string)) = "completed"
						code := http.StatusOK
						*(dest[1].(**int)) = &code
						*(dest[3].(*[]byte)) = []byte(`invalid-json`)
						*(dest[4].(*string)) = idempotency.HashPayload([]byte(`{}`))
						return nil
					},
				}
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-unmarshal-error", `{}`, nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})

	t.Run("save response db error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				}
				if strings.Contains(sql, "UPDATE") {
					return pgconn.CommandTag{}, fmt.Errorf("db update error")
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}
		r := setupRouter(conn)
		w := postRequest(r, "key-update-error", `{}`, nil)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("delete key db error", func(t *testing.T) {
		conn := &mockPgxConn{
			execFunc: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT") {
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				}
				if strings.Contains(sql, "DELETE") && !strings.Contains(sql, "expires_at") {
					return pgconn.CommandTag{}, fmt.Errorf("db delete error")
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}
		r := setupRouter(conn)
		req := httptest.NewRequest(http.MethodPost, "/admin/fail", bytes.NewBufferString(`{}`))
		req.Header.Set("Idempotency-Key", "key-delete-error")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})
}
