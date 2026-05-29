package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultIdempotencyTTL is the default cache duration for idempotency keys (24 hours).
const DefaultIdempotencyTTL = 24 * time.Hour

// idempotencyResponseWriter intercepts the written response status code and body so we can store them.
type idempotencyResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write intercepts the write of response bytes.
func (w *idempotencyResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// WriteString intercepts the write of response strings.
func (w *idempotencyResponseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Idempotency returns a Gin middleware that enforces idempotency based on the Idempotency-Key header.
func Idempotency(store IdempotencyStore) gin.HandlerFunc {
	if store == nil {
		store = NewInMemoryIdempotencyStore()
	}

	return func(c *gin.Context) {
		// Bypass idempotency check for non-mutating HTTP methods
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
			c.Abort()
			return
		}

		if len(key) > 255 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key is too long"})
			c.Abort()
			return
		}

		// Scope keys per caller/tenant
		tenantID, _ := c.Get("tenantID")
		callerID, _ := c.Get("callerID")
		var scope string
		if tenantID != nil && callerID != nil {
			scope = fmt.Sprintf("%v:%v", tenantID, callerID)
		} else if tenantID != nil {
			scope = fmt.Sprintf("%v:anonymous", tenantID)
		} else if callerID != nil {
			scope = fmt.Sprintf("anonymous:%v", callerID)
		} else {
			scope = "anonymous"
		}

		// Read and hash request body to bind key to payload
		var bodyBytes []byte
		if c.Request.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
				c.Abort()
				return
			}
			// Put body back so downstream handlers can read it
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}
		hash := sha256.Sum256(bodyBytes)
		payloadHash := hex.EncodeToString(hash[:])

		path := c.Request.URL.Path

		// Lookup or reserve key
		statusCode, responseBody, isReplay, isInFlight, err := store.GetOrInsert(
			c.Request.Context(), scope, key, method, path, payloadHash, DefaultIdempotencyTTL,
		)

		if err != nil {
			if errors.Is(err, ErrRequestMismatch) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Idempotency-Key reused with a different request"})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "idempotency check failed"})
			c.Abort()
			return
		}

		if isInFlight {
			// Return conflict response for concurrent in-flight duplicates
			c.JSON(http.StatusConflict, gin.H{"error": "Concurrent request in progress"})
			c.Abort()
			return
		}

		if isReplay {
			// Return stored original response for completed duplicate requests
			c.Header("Idempotency-Replayed", "true")
			c.Data(statusCode, "application/json; charset=utf-8", responseBody)
			c.Abort()
			return
		}

		// First request: execute downstream handler and capture response
		w := &idempotencyResponseWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		originalWriter := c.Writer
		c.Writer = w

		defer func() {
			c.Writer = originalWriter
		}()

		c.Next()

		respStatusCode := c.Writer.Status()
		if respStatusCode >= 200 && respStatusCode < 300 {
			// Success: persist the response
			_ = store.UpdateResponse(c.Request.Context(), scope, key, respStatusCode, w.body.Bytes())
		} else {
			// Failure: delete key so client can retry execution
			_ = store.Delete(c.Request.Context(), scope, key)
		}
	}
}
