package idempotency

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	headerKey     = "Idempotency-Key"
	maxKeyLength  = 255
	inflightWait  = 10 * time.Second
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

// DBMiddleware returns a Gin middleware that enforces idempotency using a PostgreSQL DBStore.
// It rejects missing/oversized keys, scopes keys per caller/tenant/admin identity,
// handles concurrent duplicate conflicts with 409, and replays completed responses.
func DBMiddleware(store *DBStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		key := strings.TrimSpace(c.GetHeader(headerKey))
		if key == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "Idempotency-Key header is required",
			})
			return
		}

		if len(key) > maxKeyLength {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "Idempotency-Key exceeds maximum length of 255 characters",
			})
			return
		}

		// Scope keys per caller/tenant/admin identity.
		tenantID := c.GetString("tenantID")
		if tenantID == "" {
			tenantID = c.GetHeader("X-Tenant-ID")
		}
		callerID := c.GetString("callerID")
		if callerID == "" {
			callerID = c.GetHeader("X-Admin-User")
		}
		if callerID == "" {
			callerID = c.GetHeader("X-Role")
		}
		if callerID == "" {
			callerID = "anonymous"
		}
		scope := fmt.Sprintf("%s:%s", tenantID, callerID)

		// Read and restore the request body.
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		payloadHash := HashPayload(bodyBytes)

		ctx := c.Request.Context()

		// Atomically check/acquire lock
		res, err := store.Acquire(ctx, scope, key, payloadHash)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency check failed: " + err.Error()})
			return
		}

		switch res.Status {
		case "in_flight_duplicate":
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": "Concurrent request in progress",
			})
			return

		case "payload_mismatch":
			c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
				"error": "Idempotency-Key reused with a different request payload",
			})
			return

		case "completed":
			c.Header("Idempotency-Replayed", "true")
			for k, values := range res.ResponseHeaders {
				for _, v := range values {
					c.Writer.Header().Set(k, v)
				}
			}
			c.Data(res.ResponseCode, "application/json; charset=utf-8", res.ResponseBody)
			c.Abort()
			return

		case "in_flight":
			// We successfully acquired the lock. Set up a custom response recorder.
			rec := &responseRecorder{ResponseWriter: c.Writer, status: http.StatusOK}
			c.Writer = rec

			// If the handler panics or fails, we delete the key to allow client retry.
			success := false
			defer func() {
				if !success {
					_ = store.DeleteKey(ctx, scope, key)
				}
			}()

			c.Next()

			// Only cache successful 2xx responses.
			if rec.status >= 200 && rec.status < 300 {
				headers := make(map[string][]string)
				for k, v := range c.Writer.Header() {
					headers[k] = v
				}
				_ = store.SaveResponse(ctx, scope, key, rec.status, rec.body.Bytes(), headers)
				success = true
			} else {
				// Delete the key on error to allow retries.
				_ = store.DeleteKey(ctx, scope, key)
				success = true
			}
		}
	}
}
