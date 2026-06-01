# Soroban Event Decoder Fixture Workflow

## Overview

This document describes the workflow for managing Soroban event fixtures used in backend integration tests.

## Fixture File Location

- Main fixture file: `internal/reconciliation/fixtures/soroban_events.json`
- This file contains golden fixtures for Soroban events emitted by the contracts

## Event Types

### Subscription Lifecycle Events

1. **subscription_created** - Emitted when a new subscription is created
2. **subscription_updated** - Emitted when subscription details change
3. **subscription_canceled** - Emitted when a subscription is canceled

### Payment Events

4. **charge_created** - Emitted when a charge is created for a subscription
5. **refund_created** - Emitted when a refund is processed

## Fixture Structure

```json
{
  "subscription_created_events": [...],
  "subscription_updated_events": [...],
  "subscription_canceled_events": [...],
  "charge_created_events": [...],
  "refund_created_events": [...],
  "malformed_events": [...]
}
```

Each event contains:
- `raw`: Base64-encoded event data (as received from Soroban)
- `decoded`: JSON with human-readable event structure
- `expected_error`: For malformed events, the expected error

## Updating Fixtures

### When to Update

1. Contract events change their schema
2. New event types are added
3. Required fields change
4. Event naming conventions change

### How to Update

1. Export events from the contracts repo
2. Convert events to base64 encoding
3. Add new events to appropriate array
4. Run tests to verify decoder compatibility

```bash
# Example: Add new subscription_created event
go run ./cmd/export-events --event-type=subscription_created --output=fixtures.json
# Then manually add to soroban_events.json
```

### Validation Steps

1. Ensure tests pass: `go test ./internal/reconciliation/...`
2. Verify all required fields are present
3. Check malformed events are still rejected

## Security Considerations

- Fixtures use mock data only
- No real user identifiers
- No real transaction hashes
- Generated addresses follow test patterns

## Running Tests

```bash
# Run all decoder tests
go test ./internal/reconciliation/... -v

# Run with coverage
go test ./internal/reconciliation/... -cover

# Run specific test
go test ./internal/reconciliation/... -run TestDecodeSubscriptionCreatedEvent -v
```

## Test Coverage Goals

- All event types: 100%
- Required field validation: 100%
- Malformed event rejection: 100%
- Edge cases: Missing fields, unknown event names, invalid types