package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	return r
}

func resetMetrics() {
	HTTPRequestDuration.Reset()
	HTTPRequestTotal.Reset()
	DBQueryDuration.Reset()
	DBQueryTotal.Reset()
}

func TestMetricsMiddleware_TracksRequest(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.GET("/test/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test/123", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/test/:id", "GET", "200")) != 1 {
		t.Error("Expected HTTPRequestTotal to be 1")
	}

	durationCount := testutil.CollectAndCount(HTTPRequestDuration)
	if durationCount == 0 {
		t.Error("Expected HTTPRequestDuration to have observations")
	}
}

func TestMetricsMiddleware_TracksDifferentStatuses(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test"})
	})
	router.GET("/notfound", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/error", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/error", "GET", "500")) != 1 {
		t.Error("Expected HTTPRequestTotal for 500 status to be 1")
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/notfound", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/notfound", "GET", "404")) != 1 {
		t.Error("Expected HTTPRequestTotal for 404 status to be 1")
	}
}

func TestMetricsMiddleware_TracksDifferentMethods(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{})
	})
	router.PUT("/test/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})
	router.DELETE("/test/:id", func(c *gin.Context) {
		c.JSON(http.StatusNoContent, gin.H{})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/test", "POST", "201")) != 1 {
		t.Error("Expected HTTPRequestTotal for POST to be 1")
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/test/123", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/test/:id", "PUT", "200")) != 1 {
		t.Error("Expected HTTPRequestTotal for PUT to be 1")
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/test/123", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/test/:id", "DELETE", "204")) != 1 {
		t.Error("Expected HTTPRequestTotal for DELETE to be 1")
	}
}

func TestMetricsMiddleware_UnknownRoute(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.GET("/known", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/unknown", nil)
	router.ServeHTTP(w, req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("unknown", "GET", "404")) != 1 {
		t.Error("Expected HTTPRequestTotal for unknown route to be 1")
	}
}

func TestDBTimer_Success(t *testing.T) {
	resetMetrics()

	done := DBTimer("SELECT", "users")
	time.Sleep(1 * time.Millisecond)
	done(nil)

	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("SELECT", "users", "false")) != 1 {
		t.Error("Expected DBQueryTotal for successful query to be 1")
	}

	durationCount := testutil.CollectAndCount(DBQueryDuration)
	if durationCount == 0 {
		t.Error("Expected DBQueryDuration to have observations")
	}
}

func TestDBTimer_Error(t *testing.T) {
	resetMetrics()

	done := DBTimer("INSERT", "orders")
	time.Sleep(1 * time.Millisecond)
	done(errors.New("connection failed"))

	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("INSERT", "orders", "true")) != 1 {
		t.Error("Expected DBQueryTotal for failed query to be 1")
	}
}

func TestRecordDBQuery(t *testing.T) {
	resetMetrics()

	RecordDBQuery("UPDATE", "products", 50*time.Millisecond, nil)

	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("UPDATE", "products", "false")) != 1 {
		t.Error("Expected DBQueryTotal for UPDATE to be 1")
	}

	RecordDBQuery("DELETE", "products", 10*time.Millisecond, errors.New("not found"))

	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("DELETE", "products", "true")) != 1 {
		t.Error("Expected DBQueryTotal for failed DELETE to be 1")
	}
}

func TestSanitizeLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "unknown"},
		{"normal", "normal"},
		{"/api/v1/users", "/api/v1/users"},
	}

	for _, tt := range tests {
		result := sanitizeLabel(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeLabel(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestSanitizeLabel_LongValue(t *testing.T) {
	longValue := strings.Repeat("a", 200)
	result := sanitizeLabel(longValue)

	if len(result) != 128 {
		t.Errorf("Expected truncated length 128, got %d", len(result))
	}
}

func TestMetricsMiddleware_MultipleRequests(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.GET("/count", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/count", nil)
		router.ServeHTTP(w, req)
	}

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/count", "GET", "200")) != 5 {
		t.Errorf("Expected HTTPRequestTotal to be 5, got %f",
			testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/count", "GET", "200")))
	}
}

func TestPrometheusRegistration(t *testing.T) {
	metrics := []prometheus.Collector{
		HTTPRequestDuration,
		HTTPRequestTotal,
		DBQueryDuration,
		DBQueryTotal,
	}

	for _, m := range metrics {
		err := testutil.CollectAndCompare(m, strings.NewReader(""))
		if err != nil && !strings.Contains(err.Error(), "expected metric") {
			t.Errorf("Metric collection failed: %v", err)
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	resetMetrics()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/metrics", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	expectedMetrics := []string{
		"http_request_duration_seconds",
		"http_requests_total",
		"db_query_duration_seconds",
		"db_queries_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("Expected metrics output to contain %s", metric)
		}
	}
}

func TestDBTimer_DifferentOperations(t *testing.T) {
	resetMetrics()

	operations := []struct {
		op    string
		table string
		err   error
	}{
		{"SELECT", "users", nil},
		{"INSERT", "users", nil},
		{"UPDATE", "users", errors.New("conflict")},
		{"DELETE", "logs", nil},
	}

	for _, op := range operations {
		done := DBTimer(op.op, op.table)
		time.Sleep(100 * time.Microsecond)
		done(op.err)
	}

	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("SELECT", "users", "false")) != 1 {
		t.Error("Expected SELECT to be recorded")
	}
	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("INSERT", "users", "false")) != 1 {
		t.Error("Expected INSERT to be recorded")
	}
	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("UPDATE", "users", "true")) != 1 {
		t.Error("Expected failed UPDATE to be recorded")
	}
	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("DELETE", "logs", "false")) != 1 {
		t.Error("Expected DELETE to be recorded")
	}
}

func TestHighCardinalityProtection(t *testing.T) {
	resetMetrics()
	router := setupTestRouter()
	router.GET("/test/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})

	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test/"+string(rune('a'+i%26)), nil)
		router.ServeHTTP(w, req)
	}

	count := testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/test/:id", "GET", "200"))
	if count != 100 {
		t.Errorf("Expected 100 requests on route pattern, got %f", count)
	}
}
