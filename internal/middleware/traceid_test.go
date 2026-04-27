package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTraceIDMiddleware_GeneratesTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceIDMiddleware())

	var capturedTraceID string
	r.GET("/test", func(c *gin.Context) {
		capturedTraceID = c.GetString("traceID")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if capturedTraceID == "" {
		t.Error("Expected trace ID to be generated")
	}

	// Check header is set
	headerTraceID := w.Header().Get("X-Trace-ID")
	if headerTraceID != capturedTraceID {
		t.Errorf("Expected trace ID in header to match context, got %s vs %s", headerTraceID, capturedTraceID)
	}
}

func TestTraceIDMiddleware_UsesProvidedTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceIDMiddleware())

	var capturedTraceID string
	r.GET("/test", func(c *gin.Context) {
		capturedTraceID = c.GetString("traceID")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Trace-ID", "custom-trace-id-123")
	r.ServeHTTP(w, req)

	if capturedTraceID != "custom-trace-id-123" {
		t.Errorf("Expected custom trace ID, got %s", capturedTraceID)
	}

	headerTraceID := w.Header().Get("X-Trace-ID")
	if headerTraceID != "custom-trace-id-123" {
		t.Errorf("Expected custom trace ID in response header, got %s", headerTraceID)
	}
}

func TestTraceIDMiddleware_SetsResponseHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TraceIDMiddleware())

	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	headerTraceID := w.Header().Get("X-Trace-ID")
	if headerTraceID == "" {
		t.Error("Expected X-Trace-ID header in response")
	}
}

