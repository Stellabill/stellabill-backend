package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestServerTimingMiddleware_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ServerTimingMiddleware())
	router.GET("/test", func(c *gin.Context) {
		rec := RecorderFromGinContext(c)
		assert.NotNil(t, rec)

		rec.RecordDB(10 * time.Millisecond)
		rec.RecordCache(2 * time.Millisecond)
		rec.RecordOutbox(5 * time.Millisecond)

		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	header := w.Header().Get("Server-Timing")
	assert.NotEmpty(t, header)
	assert.Contains(t, header, "db;dur=10.000")
	assert.Contains(t, header, "cache;dur=2.000")
	assert.Contains(t, header, "outbox;dur=5.000")
	assert.Contains(t, header, "total;dur=")
}

func TestServerTimingMiddleware_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ServerTimingMiddleware())
	router.GET("/error", func(c *gin.Context) {
		rec := RecorderFromGinContext(c)
		rec.RecordDB(1 * time.Millisecond)
		c.AbortWithStatus(http.StatusInternalServerError)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/error", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	header := w.Header().Get("Server-Timing")
	assert.NotEmpty(t, header)
	assert.Contains(t, header, "db;dur=1.000")
	assert.Contains(t, header, "cache;dur=0.000")
	assert.Contains(t, header, "outbox;dur=0.000")
	assert.Contains(t, header, "total;dur=")
}

func TestServerTimingMiddleware_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ServerTimingMiddleware())
	router.GET("/empty", func(c *gin.Context) {
		// Do nothing, just return 200
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/empty", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	header := w.Header().Get("Server-Timing")
	assert.NotEmpty(t, header)
	assert.Contains(t, header, "db;dur=0.000")
}

func TestRecorderExtraction_NilContext(t *testing.T) {
	var ctx context.Context
	assert.Nil(t, RecorderFromContext(ctx))

	var c *gin.Context
	assert.Nil(t, RecorderFromGinContext(c))
}
