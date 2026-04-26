package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/logger"
	"stellarbill-backend/internal/security"
)

const (
	AuthSubjectKey = "auth_subject"
)

type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	clients map[string]rateLimitEntry
}

type rateLimitEntry struct {
	count   int
	expires time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		clients: make(map[string]rateLimitEntry),
	}
}

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := sanitizeRequestID(c.GetHeader(RequestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}

		c.Set(RequestIDKey, requestID)
		c.Writer.Header().Set(RequestIDHeader, requestID)
		c.Next()
	}
}

func Recovery(_ *log.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		requestID, _ := c.Get(RequestIDKey)
		msg := fmt.Sprintf("panic recovered request_id=%v err=%v", requestID, recovered)
		// Use safe logger that redacts PII
		logger.SafePrintf(msg)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":      "internal server error",
			"request_id": requestID,
		})
	})
}

func Logging(_ *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get(RequestIDKey)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		msg := fmt.Sprintf(
			"method=%s path=%s status=%d request_id=%v duration=%s",
			c.Request.Method,
			security.MaskPII(path),
			c.Writer.Status(),
			requestID,
			time.Since(start).Round(time.Millisecond),
		)
		logger.SafePrintf("%s", msg)
	}
}

func CORS(allowOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := allowOrigin
		if origin == "" {
			origin = "*"
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", RequestIDHeader)
		c.Header("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || limiter.Allow(c.ClientIP()) {
			c.Next()
			return
		}

		requestID, _ := c.Get(RequestIDKey)
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error":      "rate limit exceeded",
			"request_id": requestID,
		})
	}
}

func Auth(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer"))
		if token == "" || token != jwtSecret {
			requestID, _ := c.Get(RequestIDKey)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":      "unauthorized",
				"request_id": requestID,
			})
			return
		}

		c.Set(AuthSubjectKey, "api-client")
		c.Next()
	}
}

func (r *RateLimiter) Allow(key string) bool {
	if r == nil {
		return true
	}

	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.clients[key]
	if entry.expires.Before(now) {
		entry = rateLimitEntry{
			count:   0,
			expires: now.Add(r.window),
		}
	}

	if entry.count >= r.limit {
		r.clients[key] = entry
		return false
	}

	entry.count++
	r.clients[key] = entry
	return true
}

func DeprecationHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Deprecation", "true")
		c.Header("Sunset", time.Now().Add(180*24*time.Hour).Format(time.RFC1123))

		// Build Link header pointing to the v1 equivalent of this route.
		// Requests to /api/foo become </api/v1/foo>; rel="successor-version".
		path := c.Request.URL.Path
		const prefix = "/api"
		if strings.HasPrefix(path, prefix) {
			successor := prefix + "/v1" + path[len(prefix):]
			c.Header("Link", `<`+successor+`>; rel="successor-version"`)
		}

		c.Next()
	}
}
