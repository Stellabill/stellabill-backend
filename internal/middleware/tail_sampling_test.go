package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"stellarbill-backend/internal/middleware"
)

func TestTailSamplingSignalsAnnotatesServerSpan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	router := gin.New()
	router.Use(otelgin.Middleware("test"))
	router.Use(middleware.TailSamplingSignals())
	router.GET("/failure", func(c *gin.Context) {
		c.Status(http.StatusServiceUnavailable)
	})

	request := httptest.NewRequest(http.MethodGet, "/failure", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.True(t, boolAttribute(spans[0].Attributes(), "error"))
	assert.EqualValues(t, http.StatusServiceUnavailable, intAttribute(spans[0].Attributes(), "http.response.status_code"))
}

func boolAttribute(attributes []attribute.KeyValue, key string) bool {
	for _, attr := range attributes {
		if string(attr.Key) == key {
			return attr.Value.AsBool()
		}
	}
	return false
}

func intAttribute(attributes []attribute.KeyValue, key string) int64 {
	for _, attr := range attributes {
		if string(attr.Key) == key {
			return attr.Value.AsInt64()
		}
	}
	return 0
}
