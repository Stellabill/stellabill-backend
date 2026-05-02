# Webhook Integration Guide

This guide shows how to integrate the webhook signature verification middleware into the Stellabill backend.

## Prerequisites

- Go 1.22+
- Understanding of Gin framework
- Webhook provider credentials (Stripe, PayPal, etc.)

## Quick Integration

### Step 1: Configure Webhook Provider

Add to your `.env` file:

```bash
# For Stripe
STRIPE_WEBHOOK_SECRET=whsec_your_stripe_webhook_secret

# For PayPal
PAYPAL_WEBHOOK_SECRET=your_paypal_webhook_secret

# For custom webhook
WEBHOOK_SECRET=your_webhook_secret
```

### Step 2: Create Webhook Route

Edit `cmd/server/main.go` or create a new route file:

```go
package main

import (
    "log"
    "os"
    
    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/middleware"
)

func setupWebhookRoutes(router *gin.Engine) {
    // Stripe webhook
    stripeCfg := middleware.ProviderConfig(middleware.ProviderStripe)
    stripeCfg.SecretKey = os.Getenv("STRIPE_WEBHOOK_SECRET")
    
    stripeMiddleware, err := middleware.WebhookVerificationMiddleware(stripeCfg)
    if err != nil {
        log.Fatal("Failed to create Stripe webhook middleware:", err)
    }
    
    router.POST("/api/webhooks/stripe", stripeMiddleware, handleStripeWebhook)
    
    // Generic webhook (for custom providers)
    genericCfg := middleware.DefaultWebhookConfig()
    genericCfg.SecretKey = os.Getenv("WEBHOOK_SECRET")
    
    genericMiddleware, err := middleware.WebhookVerificationMiddleware(genericCfg)
    if err != nil {
        log.Fatal("Failed to create generic webhook middleware:", err)
    }
    
    router.POST("/api/webhooks/generic", genericMiddleware, handleGenericWebhook)
}

func handleStripeWebhook(c *gin.Context) {
    eventID := c.GetString("webhook_event_id")
    rawBody := c.Get("webhook_raw_body").([]byte)
    
    log.Printf("Processing Stripe webhook %s", eventID)
    
    // Process the webhook event
    // ... your logic here ...
    
    c.JSON(http.StatusOK, gin.H{
        "status": "received",
        "event_id": eventID,
    })
}

func handleGenericWebhook(c *gin.Context) {
    provider := c.GetString("webhook_provider")
    eventID := c.GetString("webhook_event_id")
    
    log.Printf("Processing %s webhook %s", provider, eventID)
    
    c.JSON(http.StatusOK, gin.H{
        "status": "received",
        "provider": provider,
        "event_id": eventID,
    })
}
```

### Step 3: Update Main Function

```go
func main() {
    cfg, err := config.Load()
    // ... existing code ...
    
    router := gin.New()
    
    // ... existing middleware setup ...
    
    // Setup webhook routes
    setupWebhookRoutes(router)
    
    // ... rest of your routes ...
    
    router.Run()
}
```

## Real-World Examples

### Example 1: Stripe Payment Processing

```go
func handleStripeWebhook(c *gin.Context) {
    eventID := c.GetString("webhook_event_id")
    rawBody := c.Get("webhook_raw_body").([]byte)
    
    // Parse Stripe event
    var stripeEvent struct {
        Type string `json:"type"`
        Data struct {
            Object struct {
                ID     string `json:"id"`
                Amount int64  `json:"amount"`
                Status string `json:"status"`
            } `json:"object"`
        } `json:"data"`
    }
    
    if err := json.Unmarshal(rawBody, &stripeEvent); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
        return
    }
    
    // Handle specific event types
    switch stripeEvent.Type {
    case "payment_intent.succeeded":
        handlePaymentSucceeded(stripeEvent.Data.Object)
    case "payment_intent.failed":
        handlePaymentFailed(stripeEvent.Data.Object)
    case "customer.subscription.created":
        handleSubscriptionCreated(stripeEvent.Data.Object)
    default:
        log.Printf("Unhandled Stripe event type: %s", stripeEvent.Type)
    }
    
    c.JSON(http.StatusOK, gin.H{"status": "processed"})
}

func handlePaymentSucceeded(payment struct {
    ID     string
    Amount int64
    Status string
}) {
    log.Printf("Payment succeeded: %s, amount: %d", payment.ID, payment.Amount)
    // Update subscription status, send email, etc.
}
```

### Example 2: Multiple Providers

```go
func setupWebhookRoutes(router *gin.Engine) {
    providers := []struct {
        path     string
        provider middleware.WebhookProvider
        handler  gin.HandlerFunc
    }{
        {
            path:     "/stripe",
            provider: middleware.ProviderStripe,
            handler:  handleStripeWebhook,
        },
        {
            path:     "/paypal",
            provider: middleware.ProviderPayPal,
            handler:  handlePayPalWebhook,
        },
        {
            path:     "/github",
            provider: middleware.ProviderGitHub,
            handler:  handleGitHubWebhook,
        },
    }
    
    for _, p := range providers {
        cfg := middleware.ProviderConfig(p.provider)
        cfg.SecretKey = getSecretForProvider(p.provider)
        
        middleware, err := middleware.WebhookVerificationMiddleware(cfg)
        if err != nil {
            log.Fatalf("Failed to create webhook middleware for %s: %v", p.provider, err)
        }
        
        router.POST("/api/webhooks"+p.path, middleware, p.handler)
    }
}

func getSecretForProvider(provider middleware.WebhookProvider) string {
    switch provider {
    case middleware.ProviderStripe:
        return os.Getenv("STRIPE_WEBHOOK_SECRET")
    case middleware.ProviderPayPal:
        return os.Getenv("PAYPAL_WEBHOOK_SECRET")
    case middleware.ProviderGitHub:
        return os.Getenv("GITHUB_WEBHOOK_SECRET")
    default:
        return os.Getenv("WEBHOOK_SECRET")
    }
}
```

### Example 3: Custom Provider with Special Requirements

```go
func setupCustomWebhook(router *gin.Engine) {
    cfg := &middleware.WebhookConfig{
        Provider:         middleware.ProviderCustom,
        SecretKey:        os.Getenv("CUSTOM_WEBHOOK_SECRET"),
        SignatureHeader:  "X-My-Signature",
        TimestampHeader:  "X-My-Timestamp",
        EventIDHeader:    "X-My-Event-Id",
        SignatureVersion: "v3",
        Algorithm:        middleware.HMACSHA512, // Use SHA-512 for higher security
        Tolerance:        120,                   // 2 minutes tolerance
        MaxBodySize:      1024 * 1024,           // 1MB limit
        RequireTimestamp: true,
        RequireEventID:   true,
    }
    
    middleware, err := middleware.WebhookVerificationMiddleware(cfg)
    if err != nil {
        log.Fatal("Failed to create custom webhook middleware:", err)
    }
    
    router.POST("/api/webhooks/custom", middleware, handleCustomWebhook)
}
```

## Testing Your Integration

### Generate Test Signatures

```bash
# For generic HMAC
payload='{"event":"test","data":"value"}'
secret="your_secret"
signature=$(echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | sed 's/^.* //')
echo "Signature: $signature"
```

### Test with cURL

```bash
# Generic webhook
curl -X POST http://localhost:8080/api/webhooks/generic \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Signature: v2=$signature" \
  -H "X-Webhook-Timestamp: $(date +%s)" \
  -H "X-Webhook-Event-Id: $(uuidgen)" \
  -d "$payload"
```

### Test with Stripe CLI

```bash
# Install Stripe CLI
stripe login

# Forward webhooks to local server
stripe listen --forward-to localhost:8080/api/webhooks/stripe

# Trigger test event
stripe trigger payment_intent.succeeded
```

## Common Issues and Solutions

### Issue: Signature Verification Always Fails

**Solution:** Verify you're using the raw request body, not JSON-parsed body.

```go
// ❌ Wrong
var body map[string]interface{}
c.BindJSON(&body)
c.Request.Body = ioutil.NopCloser(bytes.NewBufferString(body))

// ✅ Correct
rawBody, _ := c.GetRawData()
c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(rawBody))
```

### Issue: Timestamp Tolerance Too Strict

**Solution:** Increase tolerance for clock skew.

```go
cfg.Tolerance = 600 // 10 minutes instead of 5
```

### Issue: Replay Detection Blocking Legitimate Requests

**Solution:** Increase cache TTL or disable for testing.

```go
cfg.EnableReplayProtection = false // Disable temporarily
```

## Performance Considerations

1. **Body Size**: Keep `MaxBodySize` reasonable (5-10MB typical)
2. **Algorithm**: SHA-256 is sufficient for most use cases
3. **Cache**: Monitor event ID cache size in production
4. **Async Processing**: Consider queuing non-critical webhook processing

## Security Checklist

- [ ] Use HTTPS in production
- [ ] Store secrets in secure vault/secret manager
- [ ] Rotate webhook secrets periodically
- [ ] Implement rate limiting for webhook endpoints
- [ ] Monitor for failed verification attempts
- [ ] Log all webhook delivery attempts
- [ ] Set appropriate replay cache TTL
- [ ] Validate event IDs are unique and not predictable
- [ ] Implement proper error handling (don't leak information)
- [ ] Test with invalid signatures (ensure they're rejected)

## Monitoring

Add monitoring for your webhook endpoints:

```go
func handleStripeWebhook(c *gin.Context) {
    start := time.Now()
    eventID := c.GetString("webhook_event_id")
    
    defer func() {
        duration := time.Since(start)
        log.Printf("Webhook processed: event_id=%s duration=%v", eventID, duration)
        // Send metrics to monitoring system
    }()
    
    // Process webhook...
}
```

## Next Steps

1. Review [webhook_security.md](webhook_security.md) for detailed security considerations
2. Run the test suite: `go test ./internal/middleware -run Webhook`
3. Add integration tests for your specific use case
4. Set up monitoring and alerting
5. Document your webhook endpoint API for providers
