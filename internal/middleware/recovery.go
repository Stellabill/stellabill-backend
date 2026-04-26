package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"stellarbill-backend/internal/logger"
	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
)

// ErrorResponse is the JSON envelope returned to clients when a panic is
// recovered. The shape is intentionally narrow: no panic message, no stack
// trace, no internal hints — just a stable error code, a generic message,
// the request ID for support correlation, and a server timestamp.
type ErrorResponse struct {
	Error   string    `json:"error"`
	Code    string    `json:"code"`
	Request string    `json:"request_id"`
	Time    time.Time `json:"timestamp"`
}

const (
	// maxStackBytes caps the length of stack traces we log. Anything longer
	// is truncated to keep log volume bounded under panic storms and to
	// avoid runaway memory if a panic carries an absurdly deep stack.
	maxStackBytes = 4000

	internalErrorMessage = "Internal server error"
	internalErrorCode    = "INTERNAL_ERROR"
	redactedPlaceholder  = "[REDACTED]"
)

				// Redact error and path
				redactedErr := security.RedactError(err)
				redactedPath := security.MaskPII(c.Request.URL.Path)

				logger.Log.WithFields(map[string]interface{}{
					"level":      "error",
					"request_id": requestID,
					"path":       redactedPath,
					"error":      redactedErr,
				}).Error("panic recovered")

// Recovery returns a Gin middleware that captures any panic raised by a
// downstream handler or middleware, logs a structured event with the
// request id, and writes a redacted error envelope to the client.
//
// Guarantees:
//   - The client never receives the panic value, the stack trace, or any
//     internal detail; only the static envelope above.
//   - The request id is included in both the response header and body so
//     operators can correlate the report against the server log line.
//   - Panics that occur during the recovery handler itself are caught and
//     logged at WARN; the connection is aborted instead of looping.
//   - Panics that occur after response headers are flushed cannot rewrite
//     the status code (the protocol does not allow it). The middleware
//     detects this case, logs it explicitly, and aborts the request.
//   - Stack traces are truncated to maxStackBytes and redacted for
//     credential-shaped substrings before they reach the log.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				handlePanic(c, rec, debug.Stack())
			}
		}()
		c.Next()
	}
}

func handlePanic(c *gin.Context, rec any, stack []byte) {
	// Guard against a panic from inside the recovery path itself. Without
	// this, a faulty logger or response writer would crash the goroutine
	// and tear down the connection without an error envelope.
	defer func() {
		if r2 := recover(); r2 != nil {
			logger.Log.WithFields(map[string]any{
				"request_id": GetRequestID(c),
				"path":       safePath(c),
				"panic":      redactSecrets(fmt.Sprint(r2)),
			}).Warn("panic during recovery handler — aborting connection")
			// Best effort: abort so Gin does not invoke further handlers.
			c.Abort()
		}
	}()

	requestID := GetRequestID(c)
	if requestID == "" {
		// Panics may fire before RequestID middleware ran (e.g. during the
		// middleware chain itself). Mint one so the log line is always
		// correlatable to the response the client receives.
		requestID = extractOrGenerateRequestID(c)
		c.Set(RequestIDKey, requestID)
	}
	// Always reflect the id back on the response header even when the body
	// is suppressed (e.g. when the response was already partially written).
	c.Header(RequestIDHeader, requestID)

	panicMsg := redactSecrets(fmt.Sprint(rec))
	stackStr := redactSecrets(sanitizeStack(string(stack)))

	fields := map[string]any{
		"request_id": requestID,
		"method":     c.Request.Method,
		"path":       safePath(c),
		"client_ip":  c.ClientIP(),
		"user_agent": c.Request.UserAgent(),
		"panic":      panicMsg,
		"stack":      stackStr,
	}

	if c.Writer.Written() {
		// Status and headers have already been flushed; we cannot rewrite
		// the response. Log the fact loudly so it is visible in alerts.
		fields["partial_response"] = true
		logger.Log.WithFields(fields).Error("panic after response started — connection will be aborted")
		c.Abort()
		return
	}

	logger.Log.WithFields(fields).Error("panic recovered")

	envelope := ErrorResponse{
		Error:   internalErrorMessage,
		Code:    internalErrorCode,
		Request: requestID,
		Time:    time.Now().UTC(),
	}

	if wantsPlainText(c.Request.Header.Get("Accept")) {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusInternalServerError,
			"Internal Server Error\nRequest ID: %s\n", requestID)
		c.Abort()
		return
	}

	c.JSON(http.StatusInternalServerError, envelope)
	c.Abort()
}

// wantsPlainText returns true only when the Accept header explicitly prefers
// text/plain. We default to JSON for everything else (including */* and an
// empty Accept), matching how the rest of the API responds.
func wantsPlainText(accept string) bool {
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(mediaType, "text/plain") {
			return true
		}
		if strings.EqualFold(mediaType, "application/json") {
			return false
		}
	}
	return false
}

// sanitizeStack truncates an oversized stack trace and appends a marker so
// log consumers can tell the trace was cut. Truncation is byte-based; the
// stack trace format is line-oriented so a clean cut is acceptable.
func sanitizeStack(stack string) string {
	if len(stack) <= maxStackBytes {
		return stack
	}
	return stack[:maxStackBytes] + "... (truncated)"
}

// redactSecrets walks the configured patterns and replaces any match with a
// fixed placeholder. The list is conservative: it errs toward replacing too
// much rather than letting a token slip into structured logs.
func redactSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redactedPlaceholder)
	}
	return s
}

// safePath reads c.Request.URL.Path defensively; some panic paths (e.g. a
// nil request from a misuse of the test harness) can leave the request
// pointer in an unexpected state.
func safePath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

// RecoveryLogger is retained for backward compatibility with older wiring.
// New code should use Recovery() — this alias preserves the original symbol
// so downstream code that imported the previous middleware keeps building.
//
// Deprecated: use Recovery instead.
func RecoveryLogger() gin.HandlerFunc {
	return Recovery()
}
