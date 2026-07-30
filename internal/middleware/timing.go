package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/servertiming"
)

// RecorderFromGinContext extracts the Recorder from the Gin context.
func RecorderFromGinContext(c *gin.Context) *servertiming.Recorder {
	if c == nil {
		return nil
	}
	return servertiming.FromContext(c.Request.Context())
}

// ServerTimingMiddleware intercepts the request and adds a Server-Timing header
// reflecting DB, cache, outbox, and total handler time.
func ServerTimingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		rec := &servertiming.Recorder{}

		// Inject into request context
		ctx := servertiming.WithContext(c.Request.Context(), rec)
		c.Request = c.Request.WithContext(ctx)

		// Wrap the ResponseWriter to hook before headers are written
		writer := &serverTimingResponseWriter{
			ResponseWriter: c.Writer,
			recorder:       rec,
			start:          start,
		}
		c.Writer = writer

		c.Next()

		// Ensure headers are written if handler didn't write body or status explicitly
		writer.ensureHeaderWritten()
	}
}

type serverTimingResponseWriter struct {
	gin.ResponseWriter
	recorder *servertiming.Recorder
	start    time.Time
	written  bool
}

func (w *serverTimingResponseWriter) WriteHeader(code int) {
	w.ensureHeaderWritten()
	w.ResponseWriter.WriteHeader(code)
}

func (w *serverTimingResponseWriter) Write(b []byte) (int, error) {
	w.ensureHeaderWritten()
	return w.ResponseWriter.Write(b)
}

func (w *serverTimingResponseWriter) WriteString(s string) (int, error) {
	w.ensureHeaderWritten()
	return w.ResponseWriter.WriteString(s)
}

// ensureHeaderWritten populates the Server-Timing header exactly once.
func (w *serverTimingResponseWriter) ensureHeaderWritten() {
	if w.written {
		return
	}
	w.written = true

	totalTime := time.Since(w.start)
	dbTime, cacheTime, outboxTime := w.recorder.Totals()

	// Convert to milliseconds rounded to microsecond precision.
	// E.g. dbTime.Microseconds() = 1234 -> 1.234 ms.
	headerVal := fmt.Sprintf(
		"db;dur=%.3f, cache;dur=%.3f, outbox;dur=%.3f, total;dur=%.3f",
		float64(dbTime.Microseconds())/1000.0,
		float64(cacheTime.Microseconds())/1000.0,
		float64(outboxTime.Microseconds())/1000.0,
		float64(totalTime.Microseconds())/1000.0,
	)

	// In Gin, w.ResponseWriter.Header() gets the header map.
	w.ResponseWriter.Header().Add("Server-Timing", headerVal)
}
