package tracing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"stellabill-backend/internal/tracing"
)

func TestTraceContextPropagation(t *testing.T) {
	// 1. Setup a recorder to capture spans
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)

	// 2. Clear out any global propagators for a clean test
	// (Though in production we use TraceContext)

	// 3. Setup Gin with otelgin middleware
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("test-service"))

	r.GET("/test", func(c *gin.Context) {
		// Use the request context to start a new child span
		_, span := otel.Tracer("test").Start(c.Request.Context(), "child-span")
		defer span.End()

		// Verify that the child span has the same trace ID as the parent (HTTP) span
		parentSpan := trace.SpanFromContext(c.Request.Context())
		assert.Equal(t, parentSpan.SpanContext().TraceID(), span.SpanContext().TraceID())

		c.Status(http.StatusOK)
	})

	// 4. Perform a request
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 5. Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	spans := sr.Ended()
	assert.Len(t, spans, 2) // child-span and the HTTP span

	// Ensure they share the same TraceID
	assert.Equal(t, spans[0].SpanContext().TraceID(), spans[1].SpanContext().TraceID())
}

func TestTracerExporterConfiguration(t *testing.T) {
	// Test that InitTracer doesn't panic with different configurations
	// We use "none" or "stdout" for tests to avoid external dependencies

	t.Run("stdout exporter", func(t *testing.T) {
		t.Setenv("TRACING_EXPORTER", "stdout")
		shutdown, err := tracing.InitTracer("test-stdout")
		assert.NoError(t, err)
		assert.NotNil(t, shutdown)
		_ = shutdown(context.Background())
	})

	t.Run("none exporter", func(t *testing.T) {
		t.Setenv("TRACING_EXPORTER", "none")
		shutdown, err := tracing.InitTracer("test-none")
		assert.NoError(t, err)
		assert.NotNil(t, shutdown)
		_ = shutdown(context.Background())
	})

	t.Run("invalid exporter", func(t *testing.T) {
		t.Setenv("TRACING_EXPORTER", "invalid")
		shutdown, err := tracing.InitTracer("test-invalid")
		assert.Error(t, err)
		assert.Nil(t, shutdown)
	})
}

