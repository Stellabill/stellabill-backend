package middleware

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/requestid"
)

// Re-export for backward compatibility with existing tests and call sites.
const RequestIDKey = requestid.ContextKey
const RequestIDHeader = requestid.HeaderName

const AuthSubjectKey = "auth_subject"

// RequestIDConfig holds configuration for the RequestIDWithConfig middleware.
type RequestIDConfig struct {
	// TrustedProxies is the parsed allowlist of CIDR ranges whose inbound
	// X-Request-ID header values are accepted without replacement.
	TrustedProxies []net.IPNet
}

// RequestIDWithConfig returns a Gin middleware that assigns a request_id to
// every request. If the inbound X-Request-ID header is present and the request
// originates from a trusted source, the sanitized inbound value is used.
// Otherwise a new cryptographically random ID is generated.
func RequestIDWithConfig(cfg RequestIDConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		inbound := c.GetHeader(requestid.HeaderName)
		var id string

		if inbound != "" {
			sanitized, ok := requestid.Sanitize(inbound)
			if ok && requestid.IsTrustedSource(c.Request.RemoteAddr, cfg.TrustedProxies) {
				id = sanitized
			} else {
				if ok {
					// Valid ID but untrusted source — discard and log.
					log.Printf("DEBUG: discarding spoofed request_id from %s", c.Request.RemoteAddr)
				}
				id = requestid.Generate()
			}
		} else {
			id = requestid.Generate()
		}

		c.Set(requestid.ContextKey, id)
		c.Request = c.Request.WithContext(requestid.WithRequestID(c.Request.Context(), id))
		c.Writer.Header().Set(requestid.HeaderName, id)
		c.Next()
	}
}

// RequestID is a zero-config shim around RequestIDWithConfig for backward compatibility.
func RequestID() gin.HandlerFunc {
	return RequestIDWithConfig(RequestIDConfig{})
}

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

// Recovery is defined in recovery.go with variadic logger support.

func Logging(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get(requestid.ContextKey)
		logger.Printf(
			"method=%s path=%s status=%d request_id=%v duration=%s",
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			requestID,
			time.Since(start).Round(time.Millisecond),
		)
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
		c.Header("Access-Control-Expose-Headers", requestid.HeaderName)
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

		requestID, _ := c.Get(requestid.ContextKey)
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
			requestID, _ := c.Get(requestid.ContextKey)
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
		path := c.Request.URL.Path
		const prefix = "/api"
		if strings.HasPrefix(path, prefix) {
			successor := prefix + "/v1" + path[len(prefix):]
			c.Header("Link", `<`+successor+`>; rel="successor-version"`)
		}

		c.Next()
	}
}
