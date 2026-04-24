package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/structuredlog"
)

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "request_id"
	AuthSubjectKey  = "auth_subject"
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

func Recovery(logger *log.Logger) gin.HandlerFunc {
	structured := structuredFromStandardLogger(logger)

	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		requestID, _ := c.Get(RequestIDKey)
		structured.Error("panic recovered", structuredlog.Fields{
			structuredlog.FieldRequestID: requestID,
			structuredlog.FieldActor:     actorFromContext(c),
			structuredlog.FieldTenant:    tenantFromContext(c),
			structuredlog.FieldRoute:     requestRoute(c),
			structuredlog.FieldStatus:    http.StatusInternalServerError,
			structuredlog.FieldDuration:  0,
			"error":                      recovered,
		})
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error":      "internal server error",
			"request_id": requestID,
		})
	})
}

func Logging(logger *log.Logger) gin.HandlerFunc {
	structured := structuredFromStandardLogger(logger)

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get(RequestIDKey)
		structured.Info("request completed", structuredlog.Fields{
			structuredlog.FieldRequestID: requestID,
			structuredlog.FieldActor:     actorFromContext(c),
			structuredlog.FieldTenant:    tenantFromContext(c),
			structuredlog.FieldRoute:     requestRoute(c),
			structuredlog.FieldStatus:    c.Writer.Status(),
			structuredlog.FieldDuration:  time.Since(start).Milliseconds(),
			"method":                     c.Request.Method,
		})
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

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}

	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("-_.", r):
			b.WriteRune(r)
		default:
			return ""
		}
	}
	return b.String()
}

func newRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(buf)
}

func structuredFromStandardLogger(logger *log.Logger) *structuredlog.Logger {
	if logger == nil {
		return structuredlog.New(nil)
	}
	return structuredlog.New(logger.Writer())
}

func requestRoute(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "unknown"
	}
	if route := strings.TrimSpace(c.FullPath()); route != "" {
		return route
	}
	return c.Request.URL.Path
}

func actorFromContext(c *gin.Context) string {
	if c == nil {
		return "anonymous"
	}

	for _, key := range []string{"actor", "callerID", AuthSubjectKey} {
		if value, ok := c.Get(key); ok {
			if actor, ok := value.(string); ok && strings.TrimSpace(actor) != "" {
				return actor
			}
		}
	}

	for _, header := range []string{"X-Actor", "X-User"} {
		if actor := strings.TrimSpace(c.GetHeader(header)); actor != "" {
			return actor
		}
	}

	return "anonymous"
}

func tenantFromContext(c *gin.Context) string {
	if c == nil {
		return "unknown"
	}
	if value, ok := c.Get("tenantID"); ok {
		if tenant, ok := value.(string); ok && strings.TrimSpace(tenant) != "" {
			return tenant
		}
	}
	if tenant := strings.TrimSpace(c.GetHeader("X-Tenant-ID")); tenant != "" {
		return tenant
	}
	return "unknown"
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
