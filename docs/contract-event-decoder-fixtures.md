# Contract Event Decoder Test Fixtures

This document describes the test fixtures for validating the backend's assumptions about on-chain Soroban event shapes.

## Overview

The backend ingests contract events from the Soroban blockchain. These events are validated and parsed by the ingestion package to ensure schema compatibility.

## Fixtures Location

All test fixtures are defined in `internal/ingestion/fixtures_test.go`.

## Fixture Types

### Valid Events (Positive Tests)

| Fixture Name | Event Type | Description |
|-------------|-----------|-------------|
| ValidSubscriptionCreated | contract.created | New subscription created |
| ValidSubscriptionAmended | contract.amended | Subscription plan changed |
| ValidSubscriptionRenewed | contract.renewed | Subscription renewed |
| ValidSubscriptionCancelled | contract.cancelled | Subscription cancelled |
| ValidSubscriptionExpired | contract.expired | Subscription expired |

### Invalid Events (Negative Tests)

| Fixture Name | Error Condition |
|-------------|-----------------|
| MissingIdempotencyKey | Missing idempotency_key field |
| MissingEventType | Missing event_type field |
| InvalidEventType | Unknown event_type value |
| MissingContractID | Missing contract_id field |
| MissingTenantID | Missing tenant_id field |
| MissingOccurredAt | Missing occurred_at field |
| InvalidOccurredAt | Invalid RFC 3339 format |
| InvalidPayload | Payload is not a JSON object |
| NegativeSequence | sequence_num is negative |

## Fixture Update Workflow

### When to Update Fixtures

1. Contract schema changes
2. New event types are added
3. Payload structure modifications
4. Required fields change

### Update Process

1. **Obtain new fixtures from contracts repo**
   ```bash
   # From contracts repository
   cp fixtures/*.json ../stellabill-backend/internal/ingestion/testdata/
   ```

2. **Update fixture definitions**
   - Edit `internal/ingestion/fixtures_test.go`
   - Add/update constants for each event type

3. **Run tests**
   ```bash
   go test ./internal/ingestion/... -v
   ```

4. **Verify edge cases**
   - Missing required fields
   - Unknown event names
   - Invalid types

## Security Considerations

- Malformed events must be rejected
- Invalid payloads must not corrupt accounting
- Sequence numbers must be validated to prevent replay
- Idempotency keys must prevent duplicate processing

## Test Coverage

All parser code paths are tested including:
- Valid event parsing
- Missing field detection
- Invalid format handling
- Whitespace trimming
- Payload validation

## Updating from Contract Repository

When the contract events change:

1. Check the contracts repository for updated event schemas
2. Copy new fixture JSON files
3. Update fixture constants in `fixtures_test.go`
4. Run all ingestion tests
5. Update this documentation