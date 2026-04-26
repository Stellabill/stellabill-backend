# Webhook Security and Signature Verification

This document describes the webhook signature verification middleware implementation for securing inbound provider callbacks.

## Overview

The webhook verification middleware provides security for inbound webhooks from third-party providers (Stripe, PayPal, GitHub, Square, custom) by:

1. **Signature Verification**: HMAC-based signature validation
2. **Replay Protection**: Timestamp tolerance and event ID deduplication
3. **Body Integrity**: Verifies request body before JSON parsing
4. **Provider Flexibility**: Per-provider configuration support

## Features

- ✅ **HMAC Signature Verification** (SHA-256, SHA-384, SHA-512)
- ✅ **Replay Attack Prevention** via timestamp tolerance
- ✅ **Event ID Deduplication** with configurable TTL
- ✅ **Provider-Specific Configs** (Stripe, PayPal, GitHub, Square, Generic)
- ✅ **Composite Signature Support** (e.g., Stripe's `t=timestamp,v1=signature`)
- ✅ **Body Size Limiting** to prevent DoS
- ✅ **Thread-Safe** Event ID cache
- ✅ **Context Integration** for downstream handlers

## Installation

The middleware is part of the `internal/middleware` package:

```go
import "stellarbill-backend/internal/middleware"
```

## Quick Start

### Basic Usage

```go
cfg := middleware.DefaultWebhookConfig()
cfg.SecretKey = os.Getenv("WEBHOOK_SECRET")

middleware, err := middleware.WebhookVerificationMiddleware(cfg)
if err != nil {
    log.Fatal(err)
}

router.POST("/webhook", middleware, webhookHandler)
```

### Provider-Specific Configuration

```go
// Stripe
cfg := middleware.ProviderConfig(middleware.ProviderStripe)
cfg.SecretKey = os.Getenv("STRIPE_WEBHOOK_SECRET")

// GitHub
cfg := middleware.ProviderConfig(middleware.ProviderGitHub)
cfg.SecretKey = os.Getenv("GITHUB_WEBHOOK_SECRET")

middleware, _ := middleware.WebhookVerificationMiddleware(cfg)
router.POST("/webhook", middleware, handler)
```

### Custom Configuration

```go
cfg := &middleware.WebhookConfig{
    Provider:         middleware.ProviderCustom,
    SecretKey:        os.Getenv("WEBHOOK_SECRET"),
    SignatureHeader:  "X-Custom-Signature",
    TimestampHeader:  "X-Custom-Timestamp",
    EventIDHeader:    "X-Custom-Event-Id",
    Algorithm:        middleware.HMACSHA256,
    Tolerance:        300, // 5 minutes
    RequireTimestamp: true,
    RequireEventID:   true,
}

middleware, _ := middleware.WebhookVerificationMiddleware(cfg)
```

## Configuration Options

### WebhookConfig Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `Provider` | `WebhookProvider` | Yes | - | Provider type (generic, stripe, paypal, etc.) |
| `SecretKey` | `string` | Yes | - | HMAC signing secret |
| `SignatureHeader` | `string` | No | Provider-specific | HTTP header containing signature |
| `TimestampHeader` | `string` | No | Provider-specific | HTTP header containing timestamp |
| `EventIDHeader` | `string` | No | Provider-specific | HTTP header containing event ID |
| `SignatureVersion` | `string` | No | `v2` | Signature version prefix |
| `Algorithm` | `SignatureAlgorithm` | No | `HMACSHA256` | HMAC algorithm (SHA256/384/512) |
| `Tolerance` | `int64` | No | 300 | Timestamp tolerance in seconds |
| `MaxBodySize` | `uint64` | No | 5MB | Maximum request body size |
| `RequireTimestamp` | `bool` | No | `true` | Enable timestamp verification |
| `RequireEventID` | `bool` | No | `true` | Enable event ID verification |
| `EnableReplayProtection` | `bool` | No | `true` | Enable event ID cache |

### SignatureAlgorithm Constants

- `HMACSHA256` - HMAC with SHA-256 (recommended)
- `HMACSHA384` - HMAC with SHA-384
- `HMACSHA512` - HMAC with SHA-512

### WebhookProvider Constants

- `ProviderGeneric` - Generic webhook with standard headers
- `ProviderStripe` - Stripe payment webhooks
- `ProviderPayPal` - PayPal webhooks
- `ProviderSquare` - Square payment webhooks
- `ProviderGitHub` - GitHub webhooks
- `ProviderCustom` - Custom provider

## Provider Defaults

### Stripe

```go
cfg := middleware.ProviderConfig(middleware.ProviderStripe)
// SignatureHeader: "Stripe-Signature"
// Format: "t=timestamp,v1=signature"
// Algorithm: HMACSHA256
// Tolerance: 300s
// Requires: timestamp, event ID
```

### GitHub

```go
cfg := middleware.ProviderConfig(middleware.ProviderGitHub)
// SignatureHeader: "X-Hub-Signature-256"
// Format: "sha256=signature"
// Algorithm: HMACSHA256
// Tolerance: 900s
// Requires: event ID (no timestamp)
```

### PayPal

```go
cfg := middleware.ProviderConfig(middleware.ProviderPayPal)
// SignatureHeader: "PAYPAL-TRANSMISSION-SIG"
// Algorithm: HMACSHA256
// Tolerance: 600s
// Requires: timestamp, event ID
```

### Square

```go
cfg := middleware.ProviderConfig(middleware.ProviderSquare)
// SignatureHeader: "x-square-hmacsha256-signature"
// Algorithm: HMACSHA256
// Tolerance: 300s
// Requires: timestamp, event ID
```

## How It Works

### 1. Request Flow

```
1. Client sends webhook request
   ↓
2. Middleware reads raw request body
   ↓
3. Verifies body size limit
   ↓
4. Extracts signature from header
   ↓
5. Computes HMAC of body with secret key
   ↓
6. Compares signatures (constant-time)
   ↓
7. Verifies timestamp (if required)
   ↓
8. Checks event ID for replay (if required)
   ↓
9. Stores verified data in context
   ↓
10. Proceeds to handler
```

### 2. Signature Verification

**Standard Format:**
```
Signature = HMAC-SHA256(secret, request_body)
Header: X-Webhook-Signature: v2=<hex_encoded_signature>
```

**Stripe Format (Composite):**
```
SignedPayload = timestamp + "." + request_body
Signature = HMAC-SHA256(secret, signed_payload)
Header: Stripe-Signature: t=1234567890,v1=<hex_signature>
```

### 3. Timestamp Verification

Timestamps are verified against server time with tolerance:

```
valid = (now - tolerance) <= timestamp <= (now + tolerance)
```

Prevents:
- **Replay attacks**: Old webhooks can't be replayed
- **Future requests**: Rejects requests with timestamps too far in future

### 4. Replay Protection

Event IDs are tracked in an in-memory cache with TTL:

```go
cache := middleware.NewEventIDCache(5 * time.Minute)
cache.CheckAndStore(ctx, eventID) // Returns error if seen before
```

## Error Handling

### Error Types

```go
var (
    ErrInvalidSignature      // Signature doesn't match
    ErrMissingSignature      // No signature header
    ErrMissingTimestamp      // No timestamp header
    ErrMissingEventID        // No event ID header
    ErrTimestampTooOld       // Timestamp outside tolerance (past)
    ErrTimestampTooNew       // Timestamp outside tolerance (future)
    ErrReplayDetected        // Event ID already seen
    ErrBodyTooLarge          // Request body exceeds limit
    ErrInvalidConfig         // Invalid middleware configuration
)
```

### Example Error Response

```json
{
  "error": "invalid webhook signature",
  "event_id": "evt_123456",
  "provider": "stripe",
  "verified": false,
  "request_path": "/webhook"
}
```

**HTTP Status Codes:**
- `401 Unauthorized` - Signature, timestamp, or event ID verification failed
- `413 Payload Too Large` - Request body exceeds size limit
- `400 Bad Request` - Malformed request

## Context Values

Verified webhooks set the following values in the Gin context:

```go
c.Set("webhook_event_id", eventID)      // string
c.Set("webhook_provider", provider)      // string
c.Set("webhook_verified", true)          // bool
c.Set("webhook_raw_body", rawBody)       // []byte
```

### Accessing in Handlers

```go
func webhookHandler(c *gin.Context) {
    eventID := c.GetString("webhook_event_id")
    provider := c.GetString("webhook_provider")
    rawBody := c.Get("webhook_raw_body").([]byte)
    
    // Process webhook...
}
```

## Security Best Practices

### 1. Secret Management

```go
// ❌ Don't hardcode secrets
cfg.SecretKey = "my-secret-key"

// ✅ Use environment variables or secrets manager
cfg.SecretKey = os.Getenv("WEBHOOK_SECRET")
```

### 2. Timestamp Tolerance

```go
// Production: 5 minutes is usually sufficient
cfg.Tolerance = 300

// High-security: Reduce to 1-2 minutes
cfg.Tolerance = 60

// Development: Can be more lenient
cfg.Tolerance = 600
```

### 3. Body Size Limits

```go
// Prevent DoS attacks with large payloads
cfg.MaxBodySize = 5 * 1024 * 1024 // 5MB
```

### 4. Replay Protection

```go
// Always enable for payment/critical webhooks
cfg.EnableReplayProtection = true
cfg.RequireEventID = true
```

### 5. HTTPS Only

Always use HTTPS in production to prevent MITM attacks:

```go
if cfg.Env != "development" {
    // Enforce HTTPS
}
```

## Testing

### Unit Tests

```bash
go test -v ./internal/middleware -run TestWebhookVerification
```

### Test Coverage

Run with coverage:

```bash
go test -coverprofile=coverage.out ./internal/middleware
go tool cover -html=coverage.out
```

### Manual Testing

```bash
# Generate test signature
payload='{"event":"test"}'
secret="test_secret"
signature=$(echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | cut -d' ' -f2)

# Send webhook
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Signature: v2=$signature" \
  -H "X-Webhook-Timestamp: $(date +%s)" \
  -H "X-Webhook-Event-Id: $(uuidgen)" \
  -d "$payload"
```

## Integration Example

### Complete Setup

```go
package main

import (
    "log"
    "net/http"
    "os"
    
    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/middleware"
)

func main() {
    router := gin.New()
    
    // Configure webhook verification
    cfg := middleware.ProviderConfig(middleware.ProviderStripe)
    cfg.SecretKey = os.Getenv("STRIPE_WEBHOOK_SECRET")
    
    webhookMiddleware, err := middleware.WebhookVerificationMiddleware(cfg)
    if err != nil {
        log.Fatal("Failed to create webhook middleware:", err)
    }
    
    // Apply to webhook route
    router.POST("/api/webhooks/stripe", webhookMiddleware, handleStripeWebhook)
    
    // Start server
    router.Run(":8080")
}

func handleStripeWebhook(c *gin.Context) {
    eventID := c.GetString("webhook_event_id")
    rawBody := c.Get("webhook_raw_body").([]byte)
    
    log.Printf("Processing Stripe webhook %s", eventID)
    
    // Parse and process event...
    
    c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
```

## Troubleshooting

### Signature Verification Fails

1. Check secret key matches provider dashboard
2. Verify signature header name (case-sensitive)
3. Ensure raw body is preserved (no JSON parsing before verification)
4. Check for trailing newlines in payload

### Timestamp Errors

1. Verify server time is synchronized (NTP)
2. Check timezone handling
3. Increase tolerance if clock skew is expected

### Replay Detection

1. Event IDs must be unique per webhook
2. Check cache TTL matches provider retry window
3. Clear cache on application restart if needed

## Performance

- **Signature Verification**: ~100-500μs per request
- **Replay Protection**: ~50-100μs (cache lookup)
- **Memory Usage**: ~100 bytes per cached event ID

### Optimization Tips

1. Use appropriate cache TTL (don't keep events longer than needed)
2. Limit body size to prevent DoS
3. Use connection pooling for upstream calls
4. Consider async processing for non-critical webhooks

## References

- [Stripe Webhook Signatures](https://stripe.com/docs/webhooks/signatures)
- [GitHub Webhooks](https://docs.github.com/en/webhooks)
- [PayPal Webhooks](https://developer.paypal.com/docs/api-basics/notifications/webhooks/)
- [Square Webhooks](https://developer.squareup.com/docs/webhooks)

## License

See project LICENSE file.
