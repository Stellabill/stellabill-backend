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

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer|token|auth|key|secret|password|passwd|pwd)([^\w])`),
}

// Recovery returns a Gin middleware that captures any panic raised by a
// downstream handler or middleware, logs a structured event with the
// request id, and writes a redacted error envelope to the client.
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
	// Guard against a panic from inside the recovery path itself.
	defer func() {
		if r2 := recover(); r2 != nil {
			logger.Log.WithFields(map[string]any{
				"request_id": GetRequestID(c),
				"path":       safePath(c),
				"panic":      redactSecrets(fmt.Sprint(r2)),
			}).Warn("panic during recovery handler — aborting connection")
			c.Abort()
		}
	}()

	requestID := GetRequestID(c)
	if requestID == "" {
		requestID = extractOrGenerateRequestID(c)
		c.Set(RequestIDKey, requestID)
	}
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

func sanitizeStack(stack string) string {
	if len(stack) <= maxStackBytes {
		return stack
	}
	return stack[:maxStackBytes] + "... (truncated)"
}

func redactSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redactedPlaceholder)
	}
	// Also use the general PII masker
	return security.MaskPII(s)
}

func safePath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func RecoveryLogger() gin.HandlerFunc {
	return Recovery()
}
