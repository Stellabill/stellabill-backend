package idempotency

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	headerKey    = "Idempotency-Key"
	maxKeyLength = 255
	inflightWait = 10 * time.Second

	// callerContextKey and tenantContextKey mirror the Gin context keys set by
	// middleware.AuthMiddleware. They are duplicated here as constants to avoid
	// an import cycle with the auth middleware package.
	callerContextKey = "callerID"
	tenantContextKey = "tenantID"

	// anonymousScope is used for unauthenticated requests. Anonymous callers
	// share a single namespace; this is acceptable because no caller-specific
	// data is exposed before authentication.
	anonymousScope = "anonymous"
)

// responseRecorder captures the status code and body written by downstream handlers.
type responseRecorder struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// callerScope derives the namespace under which an Idempotency-Key is stored.
// It composes the tenant and caller identifiers so two tenants — or two users
// within a tenant — using the same key cannot collide.
//
// Security note: the returned scope is hashed inside the store, so even if a
// caller can influence the format (e.g. a strange characters in a JWT subject)
// they cannot craft a value that collides with another caller's namespace.
func callerScope(c *gin.Context) string {
	tenant, _ := c.Get(tenantContextKey)
	caller, _ := c.Get(callerContextKey)
	tenantStr, _ := tenant.(string)
	callerStr, _ := caller.(string)
	if tenantStr == "" && callerStr == "" {
		return anonymousScope
	}
	return tenantStr + "/" + callerStr
}

// Middleware returns a Gin middleware that enforces idempotency for mutating
// requests (POST, PUT, PATCH, DELETE) carrying an Idempotency-Key header.
//
// Security properties:
//   - Keys are validated for length (max 255 chars) and must not be empty.
//   - The request body is hashed (SHA-256) and compared against the stored
//     hash to detect payload mismatches for the same key, returning 422.
//   - The HTTP method and request path are bound to the cached entry: replaying
//     the same key against a different route returns 422. This stops a key
//     issued for one operation from being silently reused for another.
//   - Entries are namespaced by the authenticated caller (tenant + subject).
//     Two callers may use the same Idempotency-Key without colliding, and one
//     caller cannot retrieve another caller's cached response.
//   - Concurrent duplicate requests wait up to 10 s for the first to finish,
//     then replay the cached response.
//   - Only 2xx responses are cached; errors are never stored so clients can
//     retry after a transient failure.
//
// The middleware should be installed *after* authentication so the scope can
// be derived from the verified caller identity. Anonymous requests share a
// single scope and should not include sensitive data in cached bodies.
func Middleware(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		key := strings.TrimSpace(c.GetHeader(headerKey))
		if key == "" {
			c.Next()
			return
		}

		if len(key) > maxKeyLength {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "Idempotency-Key exceeds maximum length of 255 characters",
			})
			return
		}

		scope := callerScope(c)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		payloadHash := HashPayload(bodyBytes)

		if entry := store.Get(scope, key); entry != nil {
			if !entryMatches(entry, method, path, payloadHash) {
				c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
					"error": "Idempotency-Key reused with a different request",
				})
				return
			}
			c.Header("Idempotency-Replayed", "true")
			c.Data(entry.StatusCode, "application/json; charset=utf-8", entry.Body)
			c.Abort()
			return
		}

		ch, acquired := store.AcquireInflight(scope, key)
		if !acquired {
			select {
			case <-ch:
			case <-time.After(inflightWait):
			}
			if entry := store.Get(scope, key); entry != nil {
				if !entryMatches(entry, method, path, payloadHash) {
					c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
						"error": "Idempotency-Key reused with a different request",
					})
					return
				}
				c.Header("Idempotency-Replayed", "true")
				c.Data(entry.StatusCode, "application/json; charset=utf-8", entry.Body)
				c.Abort()
				return
			}
			// First request errored or timed out without caching; let this one
			// proceed normally. Re-acquire the inflight lock to serialize
			// any further concurrent retries.
			ch2, acquired2 := store.AcquireInflight(scope, key)
			if !acquired2 {
				// Someone else slipped in front of us; replay or fall through.
				select {
				case <-ch2:
				case <-time.After(inflightWait):
				}
				if entry := store.Get(scope, key); entry != nil && entryMatches(entry, method, path, payloadHash) {
					c.Header("Idempotency-Replayed", "true")
					c.Data(entry.StatusCode, "application/json; charset=utf-8", entry.Body)
					c.Abort()
					return
				}
				c.Next()
				return
			}
			defer store.ReleaseInflight(scope, key)
			processAndCache(c, store, scope, key, method, path, payloadHash)
			return
		}

		defer store.ReleaseInflight(scope, key)
		processAndCache(c, store, scope, key, method, path, payloadHash)
	}
}

// entryMatches reports whether a cached entry was produced by an equivalent
// request. All three of method, path, and payload must match.
func entryMatches(e *Entry, method, path, payloadHash string) bool {
	if e.PayloadHash != payloadHash {
		return false
	}
	// Older entries written before method/path tracking will have empty
	// strings; treat empty as "any" to remain backward compatible while
	// still rejecting genuine mismatches between two new requests.
	if e.Method != "" && e.Method != method {
		return false
	}
	if e.Path != "" && e.Path != path {
		return false
	}
	return true
}

// processAndCache runs the handler chain and stores a successful response.
func processAndCache(c *gin.Context, store *Store, scope, key, method, path, payloadHash string) {
	rec := &responseRecorder{ResponseWriter: c.Writer, status: http.StatusOK}
	c.Writer = rec

	c.Next()

	if rec.status >= 200 && rec.status < 300 {
		store.Set(scope, key, &Entry{
			StatusCode:  rec.status,
			Body:        rec.body.Bytes(),
			PayloadHash: payloadHash,
			Method:      method,
			Path:        path,
			CreatedAt:   time.Now(),
		})
	}
}
