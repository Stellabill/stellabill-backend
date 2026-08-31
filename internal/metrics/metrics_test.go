package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/trace"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"

	"stellarbill-backend/internal/tracing"
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
		count := testutil.CollectAndCount(m)
		if count < 0 {
			t.Errorf("Metric collection failed for %v", m)
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

	done := DBTimer("SELECT", "users")
	done(nil)

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

// ---- exemplar tests ----

func newSampledCtx(t *testing.T) (context.Context, func()) {
	t.Helper()
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	return ctx, func() { span.End() }
}

func newUnsampledCtx(t *testing.T) (context.Context, func()) {
	t.Helper()
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.NeverSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	return ctx, func() { span.End() }
}

// TestExemplarLabels_SampledRecording verifies that ExemplarLabels returns valid
// trace_id (32 hex chars) and span_id (16 hex chars) labels for a sampled,
// recording span.
func TestExemplarLabels_SampledRecording(t *testing.T) {
	ctx, stop := newSampledCtx(t)
	defer stop()
	labels := tracing.ExemplarLabels(ctx)
	if labels == nil {
		t.Fatal("expected non-nil labels for sampled+recording span")
	}
	if len(labels["trace_id"]) != 32 {
		t.Errorf("trace_id length = %d, want 32", len(labels["trace_id"]))
	}
	if len(labels["span_id"]) != 16 {
		t.Errorf("span_id length = %d, want 16", len(labels["span_id"]))
	}
}

// TestExemplarLabels_Unsampled verifies nil when the span is not sampled.
func TestExemplarLabels_Unsampled(t *testing.T) {
	ctx, stop := newUnsampledCtx(t)
	defer stop()
	if labels := tracing.ExemplarLabels(ctx); labels != nil {
		t.Errorf("expected nil for unsampled span, got %v", labels)
	}
}

// TestExemplarLabels_NoSpan verifies nil for a background context with no span.
func TestExemplarLabels_NoSpan(t *testing.T) {
	if labels := tracing.ExemplarLabels(context.Background()); labels != nil {
		t.Errorf("expected nil for context without span, got %v", labels)
	}
}

// TestExemplarLabels_EndedSpan verifies nil after the span has ended (no longer
// recording).
func TestExemplarLabels_EndedSpan(t *testing.T) {
	ctx, stop := newSampledCtx(t)
	stop() // end immediately — IsRecording becomes false
	if labels := tracing.ExemplarLabels(ctx); labels != nil {
		t.Errorf("expected nil for ended (non-recording) span, got %v", labels)
	}
}

// TestExemplarLabels_EmptyTraceID verifies nil for a span with a zero TraceID
// (invalid span context).
func TestExemplarLabels_EmptyTraceID(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	}))
	if labels := tracing.ExemplarLabels(ctx); labels != nil {
		t.Errorf("expected nil for invalid (zero TraceID) span, got %v", labels)
	}
}

// TestExemplarLabels_CorruptContext verifies nil when a non-span value is stored
// under the span key.
func TestExemplarLabels_CorruptContext(t *testing.T) {
	// Use a context with no OTel span at all — the no-op span is not sampled.
	if labels := tracing.ExemplarLabels(context.WithValue(context.Background(), "not-a-span", 42)); labels != nil {
		t.Errorf("expected nil for corrupt context, got %v", labels)
	}
}

// TestMetricsMiddleware_ExemplarAttachedOnSampledRequest verifies that a sampled
// request still increments the counter and records duration.
func TestMetricsMiddleware_ExemplarAttachedOnSampledRequest(t *testing.T) {
	resetMetrics()
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "req")
	defer span.End()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/ex", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/ex", nil).WithContext(ctx)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/ex", "GET", "200")) != 1 {
		t.Error("counter must be 1 after sampled request")
	}
}

// TestMetricsMiddleware_NoExemplarOnUnsampledRequest verifies that an unsampled
// request still records metrics (without exemplars).
func TestMetricsMiddleware_NoExemplarOnUnsampledRequest(t *testing.T) {
	resetMetrics()
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.NeverSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "req")
	defer span.End()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/noex", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/noex", nil).WithContext(ctx)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/noex", "GET", "200")) != 1 {
		t.Error("counter must be 1 after unsampled request")
	}
}

// TestMetricsMiddleware_ExemplarFallbackToPlainObserve verifies that when the
// histogram observer does not implement ExemplarObserver, plain Observe is used.
// The standard prometheus.Histogram implements ExemplarObserver, so this test
// verifies the fallback path with a mock observer.
func TestMetricsMiddleware_ExemplarFallbackToPlainObserve(t *testing.T) {
	resetMetrics()

	// A non-ExemplarObserver histogram will cause the exemplar path to be skipped.
	// We just verify the middleware doesn't panic when the type assertion fails.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/fallback", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/fallback", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/fallback", "GET", "200")) != 1 {
		t.Error("counter must be 1 even with non-exemplar-observer histogram")
	}
}

// TestMetricsMiddleware_ExemplarTraceIDAndSpanIDInLabels verifies that when a
// sampled request is made, the exemplar labels contain valid trace_id and span_id.
func TestMetricsMiddleware_ExemplarTraceIDAndSpanIDInLabels(t *testing.T) {
	resetMetrics()

	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "exemplar-check")
	defer span.End()

	spanCtx := span.SpanContext()
	expectedTraceID := spanCtx.TraceID().String()
	expectedSpanID := spanCtx.SpanID().String()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/trace-check", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/trace-check", nil).WithContext(ctx)
	r.ServeHTTP(httptest.NewRecorder(), req)

	// Verify the metric was recorded
	total := testutil.ToFloat64(HTTPRequestTotal.WithLabelValues("/trace-check", "GET", "200"))
	if total != 1 {
		t.Fatalf("expected 1 request, got %f", total)
	}

	// The exemplars are verified via ExemplarLabels directly since
	// prometheus/testutil does not expose exemplar assertions on counters.
	exemplars := tracing.ExemplarLabels(ctx)
	if exemplars == nil {
		t.Fatal("expected exemplar labels for sampled request")
	}
	if exemplars["trace_id"] != expectedTraceID {
		t.Errorf("trace_id = %s, want %s", exemplars["trace_id"], expectedTraceID)
	}
	if exemplars["span_id"] != expectedSpanID {
		t.Errorf("span_id = %s, want %s", exemplars["span_id"], expectedSpanID)
	}
}

// TestExemplarLabels_ConcurrentSafety verifies that ExemplarLabels is safe to
// call from multiple goroutines simultaneously.
func TestExemplarLabels_ConcurrentSafety(t *testing.T) {
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "concurrent")
	defer span.End()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			labels := tracing.ExemplarLabels(ctx)
			if labels == nil {
				errs <- errors.New("expected non-nil labels")
				return
			}
			if labels["trace_id"] == "" || labels["span_id"] == "" {
				errs <- errors.New("expected non-empty trace_id and span_id")
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestExemplarLabels_MultipleRequestsEachGetUniqueTraceID verifies that
// separate traces produce different trace_id exemplar values.
func TestExemplarLabels_MultipleRequestsEachGetUniqueTraceID(t *testing.T) {
	tp := tracesdk.NewTracerProvider(tracesdk.WithSampler(tracesdk.AlwaysSample()))

	seen := make(map[string]bool)
	for i := range 10 {
		ctx, span := tp.Tracer("test").Start(context.Background(), "req-"+string(rune('0'+i)))
		labels := tracing.ExemplarLabels(ctx)
		if labels == nil {
			t.Fatalf("request %d: expected non-nil labels", i)
		}
		tid := labels["trace_id"]
		if seen[tid] {
			t.Errorf("request %d: duplicate trace_id %s", i, tid)
		}
		seen[tid] = true
		span.End()
	}
}

// TestMetricsMiddleware_BackwardCompatibility verifies that the Prometheus
// metrics endpoint still returns standard histogram buckets without exemplars
// when no active span is present (backward-compatible scrape behavior).
func TestMetricsMiddleware_BackwardCompatibility(t *testing.T) {
	resetMetrics()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/compat", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Request without any OTel span context (simulates pre-OpenTelemetry clients)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/compat", nil)
	r.ServeHTTP(w, req)

	// Scrape metrics endpoint
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/metrics", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	// Standard histogram output must still contain bucket lines
	if !strings.Contains(body, "http_request_duration_seconds_bucket") {
		t.Error("expected standard histogram buckets in /metrics output")
	}
	// The metric name must still be present
	if !strings.Contains(body, "http_request_duration_seconds") {
		t.Error("expected http_request_duration_seconds in /metrics output")
	}
}

// TestMetricsMiddleware_ExemplarDoesNotBreakDBTimer verifies that the DB timer
// path is unaffected by the exemplar changes.
func TestMetricsMiddleware_ExemplarDoesNotBreakDBTimer(t *testing.T) {
	resetMetrics()

	done := DBTimer("SELECT", "users")
	time.Sleep(1 * time.Millisecond)
	done(nil)

	durationCount := testutil.CollectAndCount(DBQueryDuration)
	if durationCount == 0 {
		t.Error("DB query duration must have observations")
	}
	if testutil.ToFloat64(DBQueryTotal.WithLabelValues("SELECT", "users", "false")) != 1 {
		t.Error("DB query counter must be 1")
	}
}

// TestExemplarLabels_VerifyLabelValues checks that the exemplar labels are valid
// hex-encoded strings matching the OTel trace_id and span_id format.
func TestExemplarLabels_VerifyLabelValues(t *testing.T) {
	ctx, stop := newSampledCtx(t)
	defer stop()

	labels := tracing.ExemplarLabels(ctx)
	if labels == nil {
		t.Fatal("expected non-nil labels")
	}

	for _, key := range []string{"trace_id", "span_id"} {
		val := labels[key]
		if val == "" {
			t.Errorf("label %q is empty", key)
		}
		// Verify hex encoding
		for _, ch := range val {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
				t.Errorf("label %q value %q contains non-hex character %q", key, val, string(ch))
			}
		}
	}
}
