package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// timingRecorderContextKey is the context key for the timing recorder.
type timingRecorderContextKey struct{}

// ServerTimingRecorder tracks latencies for DB, cache, and outbox.
type ServerTimingRecorder struct {
	mu          sync.Mutex
	dbTotal     time.Duration
	cacheTotal  time.Duration
	outboxTotal time.Duration
}

// RecordDB adds to the total DB duration.
func (r *ServerTimingRecorder) RecordDB(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dbTotal += d
}

// RecordCache adds to the total Cache duration.
func (r *ServerTimingRecorder) RecordCache(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheTotal += d
}

// RecordOutbox adds to the total Outbox duration.
func (r *ServerTimingRecorder) RecordOutbox(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outboxTotal += d
}

// RecorderFromContext extracts the ServerTimingRecorder from the given context.
func RecorderFromContext(ctx context.Context) *ServerTimingRecorder {
	if ctx == nil {
		return nil
	}
	val := ctx.Value(timingRecorderContextKey{})
	if rec, ok := val.(*ServerTimingRecorder); ok {
		return rec
	}
	return nil
}

// RecorderFromGinContext extracts the ServerTimingRecorder from the Gin context.
func RecorderFromGinContext(c *gin.Context) *ServerTimingRecorder {
	if c == nil {
		return nil
	}
	return RecorderFromContext(c.Request.Context())
}

// ServerTimingMiddleware intercepts the request and adds a Server-Timing header
// reflecting DB, cache, outbox, and total handler time.
func ServerTimingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		rec := &ServerTimingRecorder{}

		// Inject into request context
		ctx := context.WithValue(c.Request.Context(), timingRecorderContextKey{}, rec)
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
	recorder *ServerTimingRecorder
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

	w.recorder.mu.Lock()
	dbTime := w.recorder.dbTotal
	cacheTime := w.recorder.cacheTotal
	outboxTime := w.recorder.outboxTotal
	w.recorder.mu.Unlock()

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
