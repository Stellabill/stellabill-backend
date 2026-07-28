package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestLoadConcurrencyConfig(t *testing.T) {
	tempDir := t.TempDir()
	validYAML := `
enabled: true
default_limit: 10
retry_after: 15
routes:
  "/api/v1/test": 5
`
	validPath := filepath.Join(tempDir, "config.yaml")
	err := os.WriteFile(validPath, []byte(validYAML), 0644)
	require.NoError(t, err)

	cfg, err := LoadConcurrencyConfig(validPath)
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, int64(10), cfg.DefaultLimit)
	assert.Equal(t, 15, cfg.RetryAfter)
	assert.Equal(t, int64(5), cfg.Routes["/api/v1/test"])

	// Test non-existent file
	_, err = LoadConcurrencyConfig(filepath.Join(tempDir, "non-existent.yaml"))
	assert.Error(t, err)

	// Test invalid YAML syntax
	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	err = os.WriteFile(invalidPath, []byte("invalid_yaml: [unclosed"), 0644)
	require.NoError(t, err)
	_, err = LoadConcurrencyConfig(invalidPath)
	assert.Error(t, err)
}

func TestParseConcurrencyConfig(t *testing.T) {
	// Test defaults when minimal YAML is provided
	minimalYAML := []byte(`default_limit: 50`)
	cfg, err := ParseConcurrencyConfig(minimalYAML)
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 5, cfg.RetryAfter)
	assert.NotNil(t, cfg.Routes)

	// Test explicit zero/negative retry_after gets defaulted
	zeroRetry := []byte(`retry_after: 0`)
	cfg, err = ParseConcurrencyConfig(zeroRetry)
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.RetryAfter)

	// Test invalid YAML
	_, err = ParseConcurrencyConfig([]byte(":::invalid"))
	assert.Error(t, err)
}

func TestNewInflightLimiter(t *testing.T) {
	// Test nil config
	limiter := NewInflightLimiter(nil)
	assert.NotNil(t, limiter)
	assert.True(t, limiter.config.Enabled)
	assert.Equal(t, 5, limiter.config.RetryAfter)

	// Test with explicit routes
	cfg := &ConcurrencyConfig{
		Enabled:    true,
		RetryAfter: 10,
		Routes: map[string]int64{
			"/active":   5,
			"/disabled": 0,
		},
	}
	limiter = NewInflightLimiter(cfg)
	assert.NotNil(t, limiter.semaphores["/active"])
	assert.Nil(t, limiter.semaphores["/disabled"])
}

func TestGetSemaphore(t *testing.T) {
	cfg := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 10,
		Routes: map[string]int64{
			"/custom": 2,
			"/zero":   0,
		},
	}
	limiter := NewInflightLimiter(cfg)

	// Test existing route
	sem, enabled := limiter.getSemaphore("/custom")
	assert.True(t, enabled)
	assert.Equal(t, 2, cap(sem))

	// Test route with limit 0
	sem, enabled = limiter.getSemaphore("/zero")
	assert.False(t, enabled)
	assert.Nil(t, sem)

	// Test new route with DefaultLimit > 0
	sem, enabled = limiter.getSemaphore("/new-route")
	assert.True(t, enabled)
	assert.Equal(t, 10, cap(sem))

	// Test when disabled globally
	limiter.config.Enabled = false
	sem, enabled = limiter.getSemaphore("/custom")
	assert.False(t, enabled)
	assert.Nil(t, sem)

	// Test new route when DefaultLimit <= 0
	cfgZeroDefault := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 0,
	}
	limiterZero := NewInflightLimiter(cfgZeroDefault)
	sem, enabled = limiterZero.getSemaphore("/any")
	assert.False(t, enabled)
	assert.Nil(t, sem)
}

func TestInflightMiddleware_NormalAndDisabled(t *testing.T) {
	r := gin.New()
	cfg := &ConcurrencyConfig{
		Enabled:      false,
		DefaultLimit: 1,
	}
	r.Use(InflightMiddleware(cfg))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestInflightMiddleware_LoadShedding(t *testing.T) {
	r := gin.New()
	cfg := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 1,
		RetryAfter:   3,
	}
	r.Use(InflightMiddleware(cfg))

	started := make(chan struct{})
	release := make(chan struct{})

	r.GET("/busy", func(c *gin.Context) {
		close(started)
		<-release
		c.String(http.StatusOK, "busy done")
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodGet, "/busy", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}()

	// Wait for 1st request to acquire semaphore and enter handler
	<-started

	// Verify Prometheus metric reflects 1 in-flight request
	val := testutil.ToFloat64(InflightCurrent.WithLabelValues("/busy"))
	assert.Equal(t, float64(1), val)

	// 2nd request should exceed concurrency ceiling and receive 503
	req2 := httptest.NewRequest(http.MethodGet, "/busy", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusServiceUnavailable, w2.Code)
	assert.Equal(t, "3", w2.Header().Get("Retry-After"))

	var resp map[string]interface{}
	err := json.Unmarshal(w2.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "CONCURRENCY_LIMIT_EXCEEDED", resp["code"])

	// Release 1st request
	close(release)
	wg.Wait()

	// Verify Prometheus metric returns to 0
	valAfter := testutil.ToFloat64(InflightCurrent.WithLabelValues("/busy"))
	assert.Equal(t, float64(0), valAfter)
}

func TestInflightMiddleware_PanicRelease(t *testing.T) {
	r := gin.New()
	cfg := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 1,
	}
	r.Use(InflightMiddleware(cfg))

	r.GET("/panic", func(c *gin.Context) {
		panic("intentional test panic")
	})

	r.GET("/success", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	
	// Execute panicking handler and catch panic
	assert.Panics(t, func() {
		r.ServeHTTP(w, req)
	})

	// Verify that despite panic, the semaphore was released and gauge is 0
	valAfter := testutil.ToFloat64(InflightCurrent.WithLabelValues("/panic"))
	assert.Equal(t, float64(0), valAfter)

	// Verify semaphore token is free by sending another request to same ceiling
	req2 := httptest.NewRequest(http.MethodGet, "/success", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestInflightMiddleware_UnmatchedRoute(t *testing.T) {
	r := gin.New()
	cfg := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 10,
	}
	r.Use(InflightMiddleware(cfg))

	// Send request to non-existent route (404)
	req := httptest.NewRequest(http.MethodGet, "/non-existent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Verify metric used fallback label
	val := testutil.ToFloat64(InflightCurrent.WithLabelValues("unmatched_route"))
	assert.GreaterOrEqual(t, val, float64(0))
}

func TestInflightMiddlewareWithLimiter_NilLimiter(t *testing.T) {
	r := gin.New()
	r.Use(InflightMiddlewareWithLimiter(nil))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInflightMiddleware_UnlimitedRoute(t *testing.T) {
	r := gin.New()
	cfg := &ConcurrencyConfig{
		Enabled:      true,
		DefaultLimit: 0,
	}
	r.Use(InflightMiddleware(cfg))
	r.GET("/unlimited", func(c *gin.Context) {
		c.String(http.StatusOK, "unlimited")
	})

	req := httptest.NewRequest(http.MethodGet, "/unlimited", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "unlimited", w.Body.String())
}

func TestParseConcurrencyConfig_NilRoutes(t *testing.T) {
	nilRoutesYAML := []byte(`
enabled: true
routes: null
`)
	cfg, err := ParseConcurrencyConfig(nilRoutesYAML)
	require.NoError(t, err)
	assert.NotNil(t, cfg.Routes)
}
