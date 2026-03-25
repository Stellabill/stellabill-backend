package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "request_id"
)

var (
	// Validate request ID format: alphanumeric, max 32 chars
	validRequestID = regexp.MustCompile(`^[a-zA-Z0-9]{1,32}$`)
)

// RequestID generates or propagates request IDs for tracing
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := extractOrGenerateRequestID(c)
		
		// Store in context for downstream handlers
		c.Set(RequestIDKey, requestID)
		
		// Add to response header
		c.Header(RequestIDHeader, requestID)
		
		c.Next()
	}
}

// GetRequestID retrieves the request ID from the Gin context
func GetRequestID(c *gin.Context) string {
	if id, exists := c.Get(RequestIDKey); exists {
		if requestID, ok := id.(string); ok {
			return requestID
		}
	}
	return ""
}

// extractOrGenerateRequestID extracts request ID from headers or generates a new one
func extractOrGenerateRequestID(c *gin.Context) string {
	// Try to get from incoming header first
	if incomingID := c.GetHeader(RequestIDHeader); incomingID != "" {
		if isValidRequestID(incomingID) {
			return incomingID
		}
	}
	
	// Generate new secure random ID
	return generateRequestID()
}

// isValidRequestID validates the request ID format
func isValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	return validRequestID.MatchString(id)
}

// generateRequestID generates a secure random request ID
func generateRequestID() string {
	// Generate 8 bytes = 16 hex characters
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
