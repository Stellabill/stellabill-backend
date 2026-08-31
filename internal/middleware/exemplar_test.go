package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestExemplarAwareMiddleware_SampledSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(nil)

	ctx, span := tp.Tracer("test").Start(nil, "req")
	defer span.End()

	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Exemplar-Available") != "true" {
		t.Error("expected X-Exemplar-Available: true for sampled+recording span")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExemplarAwareMiddleware_NoSpan(t *testing.T) {
	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Exemplar-Available") != "" {
		t.Error("expected no X-Exemplar-Available header for context without span")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExemplarAwareMiddleware_UnsampledSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	defer tp.Shutdown(nil)

	ctx, span := tp.Tracer("test").Start(nil, "req")
	defer span.End()

	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Exemplar-Available") != "" {
		t.Error("expected no X-Exemplar-Available header for unsampled span")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExemplarAwareMiddleware_EndedSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(nil)

	ctx, span := tp.Tracer("test").Start(nil, "req")
	span.End() // IsRecording becomes false

	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Exemplar-Available") != "" {
		t.Error("expected no X-Exemplar-Available header for ended span")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExemplarAwareMiddleware_PreservesNextHandlerStatus(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(nil)

	ctx, _ := tp.Tracer("test").Start(nil, "req")

	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/notfound", nil).WithContext(ctx)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 from next handler, got %d", w.Code)
	}
}

func TestExemplarAwareMiddleware_BackwardCompatibility(t *testing.T) {
	// Verify that the middleware does not break requests without OTel context
	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"ok":true}` {
		t.Errorf("expected body {\"ok\":true}, got %s", w.Body.String())
	}
}

func TestExemplarAwareMiddleware_InvalidSpanContext(t *testing.T) {
	// Create a context with an invalid span context (zero trace ID)
	ctx := trace.ContextWithSpanContext(nil, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{},
		SpanID:  trace.SpanID{1},
	}))

	handler := ExemplarAwareMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Exemplar-Available") != "" {
		t.Error("expected no X-Exemplar-Available header for invalid span context")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
