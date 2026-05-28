# Webhook Idempotency

## Overview

This document describes the webhook idempotency implementation that ensures webhook events are processed exactly once, even when providers retry webhooks due to network issues or timeouts.

## Problem Statement

Webhook providers (e.g., Stripe, PayPal) often retry webhook deliveries if they don't receive a successful response. Without idempotency, this can lead to:

- Duplicate side effects (e.g., charging a customer twice)
- Inconsistent state between systems
- Difficult-to-debug race conditions

## Solution

The webhook idempotency system uses provider event IDs combined with tenant scope to deduplicate webhook events:

- **Provider Event ID**: Unique identifier from the webhook provider (e.g., Stripe event ID)
- **Tenant ID**: Tenant scope to prevent cross-tenant leakage
- **Composite Key**: `tenantID:providerEventID` ensures events are unique per tenant

## Architecture

### Components

1. **Event Store**: In-memory store with TTL-based cleanup
2. **Handler**: HTTP handler that checks for duplicates before processing
3. **Deduplication Logic**: Uses composite keys for tenant-scoped uniqueness

### Flow

```
Webhook Request → Extract Event ID + Tenant → Check Store
                                              ↓
                                    Already Processed? → Yes → Return 200 OK
                                              ↓
                                              No → Process Event → Store Event → Return 202 Accepted
```

## Implementation Details

### Event Store

The event store (`internal/webhook/store.go`) provides:

- **CheckAndStore**: Atomically checks for duplicates and stores new events
- **TTL-based cleanup**: Automatically removes expired events (default 24 hours)
- **Tenant isolation**: Events are scoped by tenant to prevent cross-tenant leakage
- **Thread-safe**: Uses mutex for concurrent access

### Handler

The webhook handler (`internal/webhook/handler.go`) provides:

- **Duplicate detection**: Returns 200 OK for already-processed events
- **New event processing**: Returns 202 Accepted for new events
- **Logging**: Logs duplicate events for monitoring
- **Validation**: Validates required fields (provider_event_id, tenant_id, event_type)

### Request Format

```json
{
  "provider_event_id": "evt_1234567890",
  "tenant_id": "tenant_abc",
  "event_type": "payment.succeeded",
  "data": {
    "amount": 1000,
    "currency": "usd"
  }
}
```

### Response Format

**New Event (202 Accepted)**:
```json
{
  "status": "accepted",
  "message": "Event accepted for processing",
  "provider_event_id": "evt_1234567890"
}
```

**Duplicate Event (200 OK)**:
```json
{
  "status": "duplicate",
  "message": "Event already processed",
  "provider_event_id": "evt_1234567890"
}
```

**Invalid Request (400 Bad Request)**:
```json
{
  "error": "invalid request",
  "message": "missing required field: tenant_id"
}
```

## Security Considerations

### Tenant Isolation

The composite key (`tenantID:providerEventID`) ensures:

- Events from different tenants with the same provider event ID are treated separately
- No cross-tenant leakage of event state
- Each tenant's event processing is independent

### TTL Policy

Events are stored with a configurable TTL (default 24 hours):

- Prevents unbounded memory growth
- Allows reprocessing of very old events if needed
- Balances memory usage with deduplication window

### Concurrent Safety

The implementation uses mutex locks to ensure:

- Thread-safe access to the event store
- No race conditions during concurrent duplicate checks
- Exactly-once semantics even under high concurrency

## Configuration

### TTL Configuration

The event store TTL is configurable:

```go
store := webhook.NewStore(24 * time.Hour) // 24 hour TTL
```

Recommended TTL values:
- **Development**: 1 hour (faster cleanup, easier testing)
- **Production**: 24-48 hours (covers typical retry windows)

### Logging

The handler logs:
- New events: `[WEBHOOK] Processing new event: provider_event_id=... tenant_id=... event_type=...`
- Duplicates: `[WEBHOOK] Duplicate event received: provider_event_id=... tenant_id=... event_type=...`

## Testing

### Unit Tests

Run webhook tests:
```bash
go test ./internal/webhook/... -v
```

### Test Coverage

The implementation includes comprehensive tests for:

- **Store tests** (`store_test.go`):
  - New event detection
  - Duplicate event detection
  - Tenant isolation
  - TTL expiration
  - Concurrent access
  - Multiple events
  - Key generation

- **Handler tests** (`handler_test.go`):
  - New event handling
  - Duplicate event handling
  - Invalid request handling
  - Tenant isolation
  - Multiple events
  - Concurrent requests

### Running Tests

```bash
# Run all webhook tests
go test ./internal/webhook/... -v -cover

# Run specific test
go test ./internal/webhook/... -run TestStore_CheckAndStore_NewEvent -v
```

## Usage Example

### Setting Up the Handler

```go
package main

import (
    "stellarbill-backend/internal/webhook"
    "github.com/gin-gonic/gin"
    "time"
)

func main() {
    // Create event store with 24-hour TTL
    store := webhook.NewStore(24 * time.Hour)
    
    // Create webhook handler
    handler := webhook.NewHandler(store)
    
    // Setup routes
    router := gin.Default()
    router.POST("/webhook", handler.HandleWebhook)
    
    router.Run(":8080")
}
```

### Processing Webhooks

```bash
# Send a webhook
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "provider_event_id": "evt_1234567890",
    "tenant_id": "tenant_abc",
    "event_type": "payment.succeeded",
    "data": {"amount": 1000}
  }'

# Response: 202 Accepted
# {
#   "status": "accepted",
#   "message": "Event accepted for processing",
#   "provider_event_id": "evt_1234567890"
# }

# Retry the same webhook (simulating provider retry)
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "provider_event_id": "evt_1234567890",
    "tenant_id": "tenant_abc",
    "event_type": "payment.succeeded",
    "data": {"amount": 1000}
  }'

# Response: 200 OK (duplicate)
# {
#   "status": "duplicate",
#   "message": "Event already processed",
#   "provider_event_id": "evt_1234567890"
# }
```

## Monitoring

### Metrics to Track

- **Duplicate rate**: Percentage of webhook requests that are duplicates
- **Event store size**: Number of events currently stored
- **Processing latency**: Time to process new events
- **TTL effectiveness**: Rate of expired events

### Log Analysis

Monitor for:
- High duplicate rates (may indicate provider retry issues)
- Unexpected tenant IDs (may indicate security issues)
- Event store growth (may indicate TTL issues)

## Best Practices

### 1. Always Include Provider Event ID

Webhook providers always include a unique event ID. Always extract and use this for idempotency:

```go
// Stripe example
eventID := stripeEvent.ID
tenantID := getTenantIDFromContext(c)
```

### 2. Validate Tenant ID

Ensure the tenant ID is extracted from a trusted source (e.g., JWT token, API key):

```go
tenantID := c.GetHeader("X-Tenant-ID")
if !isValidTenant(tenantID) {
    return c.JSON(401, gin.H{"error": "invalid tenant"})
}
```

### 3. Monitor Duplicate Rates

A high duplicate rate may indicate:
- Provider retry issues
- Network problems
- Application processing delays

### 4. Configure Appropriate TTL

Set TTL based on:
- Provider retry window (typically 24-48 hours)
- Memory constraints
- Business requirements for reprocessing

## Troubleshooting

### Issue: Events Not Being Deduplicated

**Possible causes**:
- Provider event ID not being extracted correctly
- Tenant ID not being included in the key
- TTL too short (events expiring before retries)

**Solution**:
- Verify provider event ID extraction
- Check tenant ID is included in request
- Increase TTL if needed

### Issue: High Memory Usage

**Possible causes**:
- TTL too long
- High webhook volume
- Cleanup not running

**Solution**:
- Reduce TTL
- Monitor webhook volume
- Verify cleanup goroutine is running

### Issue: Cross-Tenant Event Leakage

**Possible causes**:
- Tenant ID not being used in key
- Tenant ID extraction failure

**Solution**:
- Verify tenant ID is included in composite key
- Add validation for tenant ID

## Future Enhancements

### Planned Features

1. **Persistent Storage**: Replace in-memory store with database for durability
2. **Distributed Support**: Redis or similar for multi-instance deployments
3. **Event Replay**: Ability to replay events for debugging
4. **Metrics Integration**: Prometheus metrics for monitoring
5. **Signature Validation**: Verify webhook signatures before deduplication

### Performance Improvements

1. **Batch Processing**: Process multiple events in batches
2. **Async Processing**: Process events asynchronously
3. **Caching**: Cache frequently accessed events
4. **Sharding**: Shard event store by tenant for large deployments

## References

- [Stripe Webhooks Best Practices](https://stripe.com/docs/webhooks/best-practices)
- [Idempotency in Distributed Systems](https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/)
- [Webhook Security Guide](https://github.com/ebryn/hooks/blob/master/GUIDELINES.md)
