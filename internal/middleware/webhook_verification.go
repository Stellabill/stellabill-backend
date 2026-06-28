package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	// WebhookSignatureHeader is the header name carrying the HMAC-SHA256 signature.
	WebhookSignatureHeader = "X-Webhook-Signature"

	// webhookBodyKey is the gin context key under which the raw body is stored
	// so downstream handlers can re-read it after middleware consumption.
	webhookBodyKey = "webhook_raw_body"
)

// WebhookVerification returns a middleware that validates the HMAC-SHA256
// signature on inbound webhook requests.
//
// The signature must be provided as a hex-encoded string in the
// X-Webhook-Signature header. Requests with a missing, empty, or invalid
// secret are rejected with 401. Requests with a valid secret but wrong
// signature are rejected with 401.
//
// The raw request body is buffered and stored in the gin context under
// "webhook_raw_body" so downstream handlers can decode it without
// re-reading a consumed stream.
func WebhookVerification(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "webhook_secret_not_configured",
				"message": "webhook secret is not configured on the server",
			})
			return
		}

		sig := c.GetHeader(WebhookSignatureHeader)
		if sig == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "missing_signature",
				"message": WebhookSignatureHeader + " header is required",
			})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "body_read_error",
				"message": "failed to read request body",
			})
			return
		}
		// Restore body for downstream handlers.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Set(webhookBodyKey, body)

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(sig), []byte(expected)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "invalid_signature",
				"message": "webhook signature does not match",
			})
			return
		}

		c.Next()
	}
}
