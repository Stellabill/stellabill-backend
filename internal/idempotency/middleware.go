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

// Middleware returns a Gin middleware that enforces idempotency for mutating
// requests (POST, PUT, PATCH, DELETE) carrying an Idempotency-Key header.
//
// Security notes:
//   - Keys are validated for length (max 255 chars) and must not be empty.
//   - The request body is hashed (SHA-256) and compared against the stored hash
//     to detect payload mismatches for the same key, returning 422.
//   - Concurrent duplicate requests wait up to 10 s for the first to finish,
//     then replay the cached response.
//   - Only 2xx responses are cached; errors are never stored so clients can retry.
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

		// Read and restore the request body so downstream handlers can use it.
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		payloadHash := HashPayload(bodyBytes)

		// Check for an existing cached response.
		if entry := store.Get(key); entry != nil {
			if entry.PayloadHash != payloadHash {
				c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
					"error": "Idempotency-Key reused with a different request payload",
				})
				return
			}
			c.Header("Idempotency-Replayed", "true")
			c.Data(entry.StatusCode, "application/json; charset=utf-8", entry.Body)
			c.Abort()
			return
		}

		// Handle concurrent duplicate requests.
		ch, acquired := store.AcquireInflight(key)
		if !acquired {
			// Another goroutine is processing this key — wait for it.
			select {
			case <-ch:
			case <-time.After(inflightWait):
			}
			// Replay whatever was stored (may still be nil if the first request errored).
			if entry := store.Get(key); entry != nil {
				if entry.PayloadHash != payloadHash {
					c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
						"error": "Idempotency-Key reused with a different request payload",
					})
					return
				}
				c.Header("Idempotency-Replayed", "true")
				c.Data(entry.StatusCode, "application/json; charset=utf-8", entry.Body)
				c.Abort()
				return
			}
			// First request errored; let this one proceed normally.
			c.Next()
			return
		}

		// We hold the in-flight lock — process the request and cache the result.
		defer store.ReleaseInflight(key)

		rec := &responseRecorder{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = rec

		c.Next()

		// Only cache successful responses.
		if rec.status >= 200 && rec.status < 300 {
			store.Set(key, &Entry{
				StatusCode:  rec.status,
				Body:        rec.body.Bytes(),
				PayloadHash: payloadHash,
				CreatedAt:   time.Now(),
			})
		}
	}
}

