package middleware

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/security"
)

const (
	AuthSubjectKey = "auth_subject"

	LegacyAPISunsetEnv = "LEGACY_API_SUNSET"
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

func Logging(logger *log.Logger) gin.HandlerFunc {
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
		logger.Printf("%s", msg)
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

// DeprecatedHandler marks legacy /api/* aliases as deprecated and points
// clients at the canonical /api/v1/* successor. Do not attach it to /api/v1/*
// routes.
func DeprecatedHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.EscapedPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		const legacyPrefix = "/api/"
		const canonicalPrefix = "/api/v1/"
		if !strings.HasPrefix(path, legacyPrefix) || strings.HasPrefix(path, canonicalPrefix) {
			c.Next()
			return
		}

		c.Header("Deprecation", "true")
		if sunset := legacyAPISunsetHeader(); sunset != "" {
			c.Header("Sunset", sunset)
		}
		c.Header("Link", `</api/v1`+path[len("/api"):]+`>; rel="successor-version"`)

		c.Next()
	}
}

// DeprecationHeaders is retained for existing route wiring and tests.
func DeprecationHeaders() gin.HandlerFunc {
	return DeprecatedHandler()
}

func legacyAPISunsetHeader() string {
	raw := strings.Trim(strings.TrimSpace(os.Getenv(LegacyAPISunsetEnv)), `"'`)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return ""
	}

	if t, err := http.ParseTime(raw); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(http.TimeFormat)
	}

	return ""
}
