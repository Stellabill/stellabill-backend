# Requirements Document

## Introduction

This feature enriches the `GET /api/subscriptions/:id` endpoint response in the Stellarbill Go backend.
Currently the endpoint returns a minimal placeholder. The goal is to return a fully populated subscription
detail object that includes joined plan metadata, normalized billing amount/currency fields, and a
response schema version marker — while correctly handling edge cases such as a missing related plan
and soft-deleted records.

## Glossary

- **Subscription_Handler**: The Gin HTTP handler responsible for `GET /api/subscriptions/:id`.
- **Subscription**: A record representing a customer's recurring billing agreement, identified by a unique ID.
- **Plan**: A billing plan record containing pricing, currency, interval, and descriptive metadata.
- **Plan_Metadata**: The subset of Plan fields embedded inside a Subscription detail response (`plan_id`, `name`, `amount`, `currency`, `interval`, `description`).
- **Billing_Summary**: Normalized fields derived from the Subscription record: `amount_cents` (integer), `currency` (ISO 4217 three-letter code), and `next_billing_date` (RFC 3339 timestamp string).
- **Response_Envelope**: The top-level JSON object returned by the endpoint, containing `api_version`, `data`, and optionally `warnings`.
- **Soft-Delete**: A record marked as deleted via a `deleted_at` timestamp rather than physically removed from the database.
- **Repository**: The data-access layer responsible for querying subscriptions and plans.
- **Caller**: Any authenticated HTTP client invoking `GET /api/subscriptions/:id`.

---

## Requirements

### Requirement 1: Retrieve Full Subscription Detail

**User Story:** As a Caller, I want to retrieve a subscription by ID with all relevant fields populated, so that I can display complete subscription information without making additional API calls.

#### Acceptance Criteria

1. WHEN a valid subscription ID is provided, THE Subscription_Handler SHALL return HTTP 200 with a Response_Envelope containing the subscription's `id`, `plan_id`, `customer`, `status`, `interval`, Plan_Metadata, and Billing_Summary.
2. WHEN a subscription ID that does not exist is provided, THE Subscription_Handler SHALL return HTTP 404 with a JSON body containing an `error` field describing the resource was not found.
3. IF the subscription ID path parameter is empty or malformed, THEN THE Subscription_Handler SHALL return HTTP 400 with a JSON body containing an `error` field.

---

### Requirement 2: Embed Plan Metadata in Response

**User Story:** As a Caller, I want plan details included directly in the subscription response, so that I can render plan name, pricing, and interval without a separate plan lookup.

#### Acceptance Criteria

1. WHEN a subscription is retrieved and its associated Plan exists, THE Subscription_Handler SHALL embed Plan_Metadata as a nested `plan` object within the response `data` field.
2. WHEN a subscription is retrieved and its associated Plan does not exist, THE Subscription_Handler SHALL return the subscription fields without a `plan` object and SHALL include a `warnings` array in the Response_Envelope containing the message `"plan not found"`.
3. THE Repository SHALL resolve Plan_Metadata using the subscription's `plan_id` field.

---

### Requirement 3: Include Normalized Billing Summary

**User Story:** As a Caller, I want billing amount and currency in a normalized format, so that I can perform calculations and display values consistently across currencies.

#### Acceptance Criteria

1. THE Subscription_Handler SHALL include a `billing_summary` object in the response `data` field containing `amount_cents` as an integer, `currency` as an ISO 4217 three-letter uppercase string, and `next_billing_date` as an RFC 3339 formatted string.
2. WHEN the subscription's `amount` field cannot be parsed into a valid integer number of cents, THE Subscription_Handler SHALL return HTTP 500 with a JSON body containing an `error` field and SHALL log the parse failure.
3. WHEN the subscription's `next_billing` field is absent or empty, THE Subscription_Handler SHALL set `next_billing_date` to `null` in the Billing_Summary.

---

### Requirement 4: Response Schema Versioning

**User Story:** As a Caller, I want a version marker in the response envelope, so that I can detect schema changes and adapt my client accordingly.

#### Acceptance Criteria

1. THE Subscription_Handler SHALL include an `api_version` field at the top level of the Response_Envelope set to the string `"1"`.
2. THE Subscription_Handler SHALL set the `Content-Type` response header to `application/json; charset=utf-8`.

---

### Requirement 5: Handle Soft-Deleted Subscriptions

**User Story:** As a Caller, I want requests for soft-deleted subscriptions to return a clear response, so that my client can distinguish between a missing record and a deleted one.

#### Acceptance Criteria

1. WHEN a subscription ID refers to a record where `deleted_at` is set, THE Subscription_Handler SHALL return HTTP 410 with a JSON body containing an `error` field with the value `"subscription has been deleted"`.
2. THE Repository SHALL expose the `deleted_at` field so THE Subscription_Handler can inspect it without performing a raw query.

---

### Requirement 6: Security — Authorization Enforcement

**User Story:** As a system operator, I want the endpoint to enforce caller identity, so that one customer cannot access another customer's subscription data.

#### Acceptance Criteria

1. WHEN a request is received without a valid authorization credential, THE Subscription_Handler SHALL return HTTP 401 with a JSON body containing an `error` field.
2. WHEN a valid credential is present but the authenticated identity does not own the requested subscription, THE Subscription_Handler SHALL return HTTP 403 with a JSON body containing an `error` field.
3. THE Subscription_Handler SHALL not include sensitive internal fields (such as raw database IDs or internal cost basis) in the response.

---

### Requirement 7: Unit and Integration Test Coverage

**User Story:** As a developer, I want automated tests covering the enriched response shape and edge cases, so that regressions are caught before deployment.

#### Acceptance Criteria

1. THE test suite SHALL include a unit test verifying that a subscription with a valid associated plan produces a Response_Envelope containing Plan_Metadata and Billing_Summary with correct field values.
2. THE test suite SHALL include a unit test verifying that a subscription with a missing plan produces a Response_Envelope with no `plan` object and a `warnings` array containing `"plan not found"`.
3. THE test suite SHALL include a unit test verifying that a soft-deleted subscription returns HTTP 410.
4. THE test suite SHALL include a unit test verifying that an unknown subscription ID returns HTTP 404.
5. THE test suite SHALL include an integration test that exercises `GET /api/subscriptions/:id` end-to-end and asserts the full response shape matches the Response_Envelope schema.
6. FOR ALL valid Subscription records, serializing the response to JSON and deserializing it back SHALL produce an equivalent Response_Envelope (round-trip property).
