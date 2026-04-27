package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// Webhook signature verification default settings
	DefaultSignatureHeader        = "X-Webhook-Signature"
	DefaultTimestampHeader        = "X-Webhook-Timestamp"
	DefaultEventIDHeader          = "X-Webhook-Event-Id"
	DefaultSignatureVersion       = "v2"
	DefaultMaxSignatureAge        = 5 * time.Minute
	DefaultTimestampSkew          = 300 // 5 minutes in seconds
	DefaultMaxBodySize     uint64 = 1024 * 1024 * 5 // 5MB

	// Provider-specific defaults
	StripeSignatureHeader   = "Stripe-Signature"
	StripeSignaturePrefix   = "v1="
	DefaultWebhookTolerance = 300 // 5 minutes
)

var (
	ErrInvalidSignature      = errors.New("invalid webhook signature")
	ErrMissingSignature      = errors.New("missing webhook signature")
	ErrMissingTimestamp      = errors.New("missing webhook timestamp")
	ErrMissingEventID        = errors.New("missing webhook event ID")
	ErrTimestampTooOld       = errors.New("webhook timestamp too old")
	ErrTimestampTooNew       = errors.New("webhook timestamp too new")
	ErrReplayDetected        = errors.New("webhook replay attack detected")
	ErrBodyTooLarge          = errors.New("webhook body too large")
	ErrInvalidConfig         = errors.New("invalid webhook configuration")
	ErrProviderMismatch      = errors.New("webhook provider mismatch")
	ErrSignatureFormat       = errors.New("invalid signature format")
	ErrVerificationNotConfig = errors.New("webhook verification not configured for provider")
)

// SignatureAlgorithm represents the hashing algorithm used for signature verification.
type SignatureAlgorithm string

const (
	HMACSHA256 SignatureAlgorithm = "HMAC-SHA256"
	HMACSHA384 SignatureAlgorithm = "HMAC-SHA384"
	HMACSHA512 SignatureAlgorithm = "HMAC-SHA512"
)

// WebhookProvider represents a known webhook provider with its specific configuration.
type WebhookProvider string

const (
	ProviderGeneric    WebhookProvider = "generic"
	ProviderStripe     WebhookProvider = "stripe"
	ProviderPayPal     WebhookProvider = "paypal"
	ProviderSquare     WebhookProvider = "square"
	ProviderGitHub     WebhookProvider = "github"
	ProviderCustom     WebhookProvider = "custom"
)

// WebhookConfig holds configuration for webhook signature verification for a specific provider.
type WebhookConfig struct {
	// Provider identifies the webhook provider (e.g., "stripe", "paypal", "generic")
	Provider WebhookProvider

	// SecretKey is the signing secret used to verify webhook signatures
	// Should be fetched from a secure secrets provider
	SecretKey string

	// SignatureHeader is the HTTP header containing the signature
	// Defaults based on provider if empty
	SignatureHeader string

	// TimestampHeader is the HTTP header containing the timestamp
	// Optional for some providers
	TimestampHeader string

	// EventIDHeader is the HTTP header containing the unique event ID
	// Used for replay attack prevention
	EventIDHeader string

	// SignatureVersion is the signature version prefix (e.g., "v1=", "v2=")
	// Optional, used by some providers
	SignatureVersion string

	// Algorithm is the hashing algorithm used for HMAC
	Algorithm SignatureAlgorithm

	// Tolerance is the maximum allowed time difference in seconds between
	// the webhook timestamp and current server time
	// Defaults to 300 seconds (5 minutes)
	Tolerance int64

	// MaxBodySize is the maximum request body size in bytes
	// Requests larger than this will be rejected
	MaxBodySize uint64

	// RequireTimestamp enables timestamp verification
	// If disabled, only signature verification is performed
	RequireTimestamp bool

	// RequireEventID enables event ID verification and deduplication
	// If disabled, replay protection via event ID is skipped
	RequireEventID bool

	// EnableReplayProtection enables in-memory tracking of seen event IDs
	// to prevent replay attacks
	EnableReplayProtection bool
}

// DefaultWebhookConfig returns a secure default configuration for generic webhooks.
func DefaultWebhookConfig() *WebhookConfig {
	return &WebhookConfig{
		Provider:              ProviderGeneric,
		SignatureHeader:       DefaultSignatureHeader,
		TimestampHeader:       DefaultTimestampHeader,
		EventIDHeader:         DefaultEventIDHeader,
		SignatureVersion:      DefaultSignatureVersion,
		Algorithm:             HMACSHA256,
		Tolerance:             int64(DefaultTimestampSkew),
		MaxBodySize:           DefaultMaxBodySize,
		RequireTimestamp:      true,
		RequireEventID:        true,
		EnableReplayProtection: true,
	}
}

// ProviderConfig returns a pre-configured WebhookConfig for known providers.
// The secret key must be provided separately via a secrets provider.
func ProviderConfig(provider WebhookProvider) *WebhookConfig {
	cfg := DefaultWebhookConfig()
	cfg.Provider = provider

	switch provider {
	case ProviderStripe:
		cfg.SignatureHeader = StripeSignatureHeader
		cfg.TimestampHeader = StripeSignatureHeader // Stripe includes timestamp in same header
		cfg.EventIDHeader = "Stripe-Event-Id"
		cfg.SignatureVersion = "v1"
		cfg.Algorithm = HMACSHA256
		cfg.Tolerance = DefaultWebhookTolerance
		cfg.RequireTimestamp = true
		cfg.RequireEventID = true
		cfg.EnableReplayProtection = true
	case ProviderPayPal:
		cfg.SignatureHeader = "PAYPAL-TRANSMISSION-SIG"
		cfg.TimestampHeader = "PAYPAL-TRANSMISSION-TIME"
		cfg.EventIDHeader = "PAYPAL-TRANSMISSION-ID"
		cfg.Algorithm = HMACSHA256
		cfg.Tolerance = 600 // 10 minutes
		cfg.RequireTimestamp = true
		cfg.RequireEventID = true
		cfg.EnableReplayProtection = true
	case ProviderGitHub:
		cfg.SignatureHeader = "X-Hub-Signature-256"
		cfg.TimestampHeader = "X-Hub-Request-Started"
		cfg.EventIDHeader = "X-GitHub-Delivery"
		cfg.SignatureVersion = "sha256="
		cfg.Algorithm = HMACSHA256
		cfg.Tolerance = 900 // 15 minutes
		cfg.RequireTimestamp = false
		cfg.RequireEventID = true
		cfg.EnableReplayProtection = true
	case ProviderSquare:
		cfg.SignatureHeader = "x-square-hmacsha256-signature"
		cfg.TimestampHeader = "x-square-signature-timestamp"
		cfg.EventIDHeader = "x-square-delivery-id"
		cfg.Algorithm = HMACSHA256
		cfg.Tolerance = DefaultWebhookTolerance
		cfg.RequireTimestamp = true
		cfg.RequireEventID = true
		cfg.EnableReplayProtection = true
	}

	return cfg
}

// Validate validates the webhook configuration.
func (c *WebhookConfig) Validate() error {
	if c.SecretKey == "" {
		return fmt.Errorf("%w: secret key is required", ErrInvalidConfig)
	}

	if c.Provider == "" {
		return fmt.Errorf("%w: provider must be specified", ErrInvalidConfig)
	}

	switch c.Algorithm {
	case HMACSHA256, HMACSHA384, HMACSHA512:
		// Valid algorithms
	default:
		return fmt.Errorf("%w: unsupported algorithm: %s", ErrInvalidConfig, c.Algorithm)
	}

	if c.MaxBodySize == 0 {
		c.MaxBodySize = DefaultMaxBodySize
	}

	if c.Tolerance == 0 {
		c.Tolerance = int64(DefaultTimestampSkew)
	}

	return nil
}

// WebhookVerificationMiddleware creates a Gin middleware for webhook signature verification.
// It verifies the signature, timestamp, and event ID to prevent tampering and replay attacks.
func WebhookVerificationMiddleware(cfg *WebhookConfig) (gin.HandlerFunc, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var replayCache *EventIDCache
	if cfg.EnableReplayProtection {
		replayCache = NewEventIDCache(5 * time.Minute)
	}

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Verify the request body hasn't exceeded the size limit
		if c.Request.ContentLength > int64(cfg.MaxBodySize) {
			abortWebhook(c, http.StatusRequestEntityTooLarge, ErrBodyTooLarge)
			return
		}

		// Read and store the raw body for signature verification
		rawBody, err := c.GetRawData()
		if err != nil {
			abortWebhook(c, http.StatusBadRequest, fmt.Errorf("failed to read request body: %w", err))
			return
		}

		// Restore the body so handlers can read it
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(cfg.MaxBodySize))

		// Verify signature
		signature := c.GetHeader(cfg.SignatureHeader)
		if signature == "" {
			abortWebhook(c, http.StatusUnauthorized, ErrMissingSignature)
			return
		}

		if err := verifySignature(rawBody, signature, cfg); err != nil {
			abortWebhook(c, http.StatusUnauthorized, err)
			return
		}

		// Verify timestamp (if required)
		if cfg.RequireTimestamp && cfg.TimestampHeader != "" {
			timestamp := c.GetHeader(cfg.TimestampHeader)
			if timestamp == "" {
				abortWebhook(c, http.StatusUnauthorized, ErrMissingTimestamp)
				return
			}

			if err := verifyTimestamp(timestamp, cfg.Tolerance); err != nil {
				abortWebhook(c, http.StatusUnauthorized, err)
				return
			}
		}

		// Verify event ID and check for replays (if required)
		if cfg.RequireEventID && cfg.EventIDHeader != "" {
			eventID := c.GetHeader(cfg.EventIDHeader)
			if eventID == "" {
				abortWebhook(c, http.StatusUnauthorized, ErrMissingEventID)
				return
			}

			if replayCache != nil {
				if err := replayCache.CheckAndStore(ctx, eventID); err != nil {
					abortWebhook(c, http.StatusUnauthorized, ErrReplayDetected)
					return
				}
			}

			// Store event ID in context for downstream handlers
			c.Set("webhook_event_id", eventID)
		}

		// Store provider info in context
		c.Set("webhook_provider", cfg.Provider)
		c.Set("webhook_verified", true)

		// Restore the body for downstream processing
		c.Request.Body = http.MaxBytesReader(c.Writer,
			&readSeekCloser{data: rawBody, pos: 0},
			int64(cfg.MaxBodySize),
		)

		// Bind raw body to context for handlers that need it
		c.Set("webhook_raw_body", rawBody)

		c.Next()
	}, nil
}

// verifySignature verifies the HMAC signature of the payload.
func verifySignature(payload []byte, signature string, cfg *WebhookConfig) error {
	if signature == "" {
		return ErrMissingSignature
	}

	// Remove signature version prefix if present
	if cfg.SignatureVersion != "" && strings.HasPrefix(signature, cfg.SignatureVersion) {
		signature = strings.TrimPrefix(signature, cfg.SignatureVersion)
	}

	// Handle composite signatures (e.g., Stripe format: t=timestamp,v1=signature)
	if strings.Contains(signature, ",") {
		return verifyCompositeSignature(payload, signature, cfg)
	}

	// Decode the hex-encoded signature
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: invalid hex encoding: %v", ErrInvalidSignature, err)
	}

	// Compute HMAC
	var computedHash hash.Hash
	switch cfg.Algorithm {
	case HMACSHA256:
		computedHash = hmac.New(sha256.New, []byte(cfg.SecretKey))
	case HMACSHA384:
		computedHash = hmac.New(sha512.New384, []byte(cfg.SecretKey))
	case HMACSHA512:
		computedHash = hmac.New(sha512.New, []byte(cfg.SecretKey))
	default:
		return fmt.Errorf("%w: unsupported algorithm", ErrInvalidSignature)
	}

	computedHash.Write(payload)
	computedSig := computedHash.Sum(nil)

	// Compare signatures using constant-time comparison
	if !hmac.Equal(sigBytes, computedSig) {
		return ErrInvalidSignature
	}

	return nil
}

// verifyCompositeSignature handles signatures that include metadata (e.g., Stripe format).
func verifyCompositeSignature(payload []byte, signature string, cfg *WebhookConfig) error {
	// Parse composite signature format
	parts := strings.Split(signature, ",")
	var timestamp, sig string

	for _, part := range parts {
		if strings.HasPrefix(part, "t=") {
			timestamp = strings.TrimPrefix(part, "t=")
		} else if strings.HasPrefix(part, cfg.SignatureVersion+"=") {
			sig = strings.TrimPrefix(part, cfg.SignatureVersion+"=")
		} else if strings.HasPrefix(part, "v1=") {
			sig = strings.TrimPrefix(part, "v1=")
		}
	}

	if sig == "" {
		return fmt.Errorf("%w: no signature found in composite signature", ErrInvalidSignature)
	}

	if cfg.TimestampHeader == "" && timestamp != "" {
		// Verify timestamp if extracted from signature
		if err := verifyTimestamp(timestamp, cfg.Tolerance); err != nil {
			return err
		}
	}

	// For Stripe-style signatures, sign the timestamp.payload combination
	if timestamp != "" {
		signedPayload := timestamp + "." + string(payload)
		return verifySignature([]byte(signedPayload), sig, cfg)
	}

	return verifySignature(payload, sig, cfg)
}

// verifyTimestamp verifies that the timestamp is within the allowed tolerance.
func verifyTimestamp(timestamp string, tolerance int64) error {
	// Try to parse as Unix timestamp in seconds first
	secs, err := parseUnixTimestamp(timestamp)
	if err != nil {
		return fmt.Errorf("%w: invalid timestamp format: %v", ErrInvalidSignature, err)
	}

	now := time.Now().Unix()
	minTime := now - tolerance
	maxTime := now + tolerance

	if secs < minTime {
		return ErrTimestampTooOld
	}
	if secs > maxTime {
		return ErrTimestampTooNew
	}

	return nil
}

// parseUnixTimestamp parses various timestamp formats to Unix seconds.
func parseUnixTimestamp(timestamp string) (int64, error) {
	// Try parsing as a plain integer (Unix timestamp in seconds)
	if secs, err := parseInt64(timestamp); err == nil {
		if secs > 10000000000 { // Likely milliseconds or nanoseconds
			if secs > 1000000000000 { // Nanoseconds
				return secs / 1e9, nil
			}
			return secs / 1000, nil // Milliseconds
		}
		return secs, nil
	}

	// Try parsing as ISO 8601 / RFC3339
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return 0, err
		}
	}

	return ts.Unix(), nil
}

// abortWebhook sends a standardized error response for webhook verification failures.
func abortWebhook(c *gin.Context, statusCode int, err error) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.AbortWithStatusJSON(statusCode, gin.H{
		"error":        err.Error(),
		"event_id":     c.GetString("webhook_event_id"),
		"provider":     c.GetString("webhook_provider"),
		"verified":     false,
		"request_path": c.FullPath(),
	})
}

// parseInt64 is a helper to parse int64 from string.
func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// readSeekCloser wraps a byte slice to provide an io.ReadSeekCloser.
type readSeekCloser struct {
	data []byte
	pos  int64
}

func (r *readSeekCloser) Read(p []byte) (n int, err error) {
	if r.pos >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return n, nil
}

func (r *readSeekCloser) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = r.pos + offset
	case io.SeekEnd:
		newPos = int64(len(r.data)) + offset
	}
	if newPos < 0 {
		return 0, errors.New("negative position")
	}
	r.pos = newPos
	return r.pos, nil
}

func (r *readSeekCloser) Close() error {
	return nil
}
