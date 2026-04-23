package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/requestid"
)

// ErrorResponse is the structured error envelope returned on panic recovery.
type ErrorResponse struct {
	Error   string    `json:"error"`
	Code    string    `json:"code"`
	Request string    `json:"request_id"`
	Time    time.Time `json:"time"`
}

// GetRequestID retrieves the request_id from the Gin context.
// Returns an empty string if not set.
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(requestid.ContextKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// sanitizeStack truncates a stack trace to a safe maximum length.
func sanitizeStack(stack string) string {
	const maxLen = 4000
	if len(stack) <= maxLen {
		return stack
	}
	return stack[:maxLen] + "... (truncated)"
}

// Recovery returns a Gin middleware that recovers from panics.
// When called with no arguments it uses the standard library logger.
// When called with a *log.Logger it uses that logger.
// This is the canonical no-arg version; see also Recovery(logger) in middleware.go.
func Recovery(loggers ...*log.Logger) gin.HandlerFunc {
	var logger *log.Logger
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}

	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		// Get or generate request ID
		rid := GetRequestID(c)
		if rid == "" {
			rid = c.GetHeader(requestid.HeaderName)
		}
		if rid == "" {
			rid = requestid.Generate()
		}

		// Always echo the request ID in the response header
		c.Writer.Header().Set(requestid.HeaderName, rid)

		if logger != nil {
			logger.Printf("panic recovered request_id=%v err=%v", rid, recovered)
		}

		// If headers already written, don't try to write a new response
		if c.Writer.Written() {
			return
		}

		// Check Accept header for plain text preference
		accept := c.GetHeader("Accept")
		if strings.Contains(accept, "text/plain") && !strings.Contains(accept, "application/json") {
			c.String(http.StatusInternalServerError, fmt.Sprintf(
				"Internal Server Error\nRequest ID: %s\n", rid,
			))
			return
		}

		c.AbortWithStatusJSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "internal server error",
			Code:    "INTERNAL_ERROR",
			Request: rid,
			Time:    time.Now().UTC(),
		})
	})
}

// RecoveryLogger is kept for backward compatibility.
// Prefer Recovery() or Recovery(logger) instead.
func RecoveryLogger() gin.HandlerFunc {
	return Recovery()
}
