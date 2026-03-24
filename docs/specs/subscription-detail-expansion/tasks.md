# Implementation Plan: Subscription Detail Expansion

## Overview

Enrich `GET /api/subscriptions/:id` by introducing a repository layer, a service layer, auth middleware, and an updated handler that returns a fully populated `ResponseEnvelope` with embedded plan metadata, normalized billing summary, schema versioning, and correct HTTP semantics for all edge cases.

## Tasks

- [x] 1. Define repository interfaces and data models
  - Create `internal/repository/interfaces.go` with `SubscriptionRepository` and `PlanRepository` interfaces and the `ErrNotFound` sentinel error
  - Create `internal/repository/models.go` with `SubscriptionRow` and `PlanRow` structs
  - _Requirements: 1.1, 2.3, 5.2_

- [x] 2. Define service types and error sentinels
  - Create `internal/service/types.go` with `PlanMetadata`, `BillingSummary`, `SubscriptionDetail`, and `ResponseEnvelope` structs
  - Create `internal/service/errors.go` with `ErrNotFound`, `ErrDeleted`, `ErrForbidden`, and `ErrBillingParse` sentinel errors
  - _Requirements: 1.1, 3.1, 4.1, 6.3_

- [x] 3. Implement in-memory mock repositories for testing
  - Create `internal/repository/mock.go` with `MockSubscriptionRepo` and `MockPlanRepo` that satisfy the repository interfaces
  - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [x] 4. Implement SubscriptionService
  - [x] 4.1 Create `internal/service/subscription_service.go` implementing `GetDetail(ctx, callerID, subscriptionID)`:
    - Call `SubscriptionRepo.FindByID`; return `ErrNotFound` if not found
    - Return `ErrDeleted` if `DeletedAt` is non-nil
    - Return `ErrForbidden` if `callerID != row.CustomerID`
    - Call `PlanRepo.FindByID`; attach `PlanMetadata` or append `"plan not found"` warning
    - Parse `Amount` to `int64` cents; return `ErrBillingParse` (and log) on failure
    - Build and return `SubscriptionDetail`
    - _Requirements: 1.1, 1.2, 2.1, 2.2, 3.1, 3.2, 3.3, 5.1, 6.2_

  - [ ]\* 4.2 Write property test for GetDetail — Property 1: Successful response envelope invariants
    - **Property 1: Successful response envelope invariants**
    - **Validates: Requirements 1.1, 4.1, 4.2**

  - [ ]\* 4.3 Write property test for GetDetail — Property 2: Plan metadata embedded when plan exists
    - **Property 2: Plan metadata embedded when plan exists**
    - **Validates: Requirements 2.1**

  - [ ]\* 4.4 Write property test for GetDetail — Property 3: Missing plan produces warning and no plan object
    - **Property 3: Missing plan produces warning and no plan object**
    - **Validates: Requirements 2.2**

  - [ ]\* 4.5 Write property test for GetDetail — Property 4: Billing summary normalization
    - **Property 4: Billing summary normalization**
    - **Validates: Requirements 3.1, 3.3**

  - [ ]\* 4.6 Write property test for GetDetail — Property 5: Unparseable amount yields HTTP 500
    - **Property 5: Unparseable amount yields HTTP 500**
    - **Validates: Requirements 3.2**

  - [ ]\* 4.7 Write property test for GetDetail — Property 6: Soft-deleted subscription yields HTTP 410
    - **Property 6: Soft-deleted subscription yields HTTP 410**
    - **Validates: Requirements 5.1**

  - [ ]\* 4.8 Write unit tests for SubscriptionService
    - Happy path: valid subscription + plan → full `SubscriptionDetail`
    - Missing plan → warnings, no plan field
    - Soft-deleted → `ErrDeleted`
    - Unknown ID → `ErrNotFound`
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [x] 5. Checkpoint — Ensure all service-layer tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Implement AuthMiddleware
  - Create `internal/middleware/auth.go` with `AuthMiddleware(jwtSecret string) gin.HandlerFunc`
  - Validate `Authorization: Bearer <jwt>` header; abort with 401 JSON on failure
  - Inject `callerID` into the Gin context on success
  - _Requirements: 6.1_

  - [ ]\* 6.1 Write property test for AuthMiddleware — Property 9: Missing credential yields HTTP 401
    - **Property 9: Missing credential yields HTTP 401**
    - **Validates: Requirements 6.1**

- [x] 7. Update GetSubscription handler
  - Modify `internal/handlers/subscriptions.go` to:
    - Accept `SubscriptionService` as a dependency
    - Read `callerID` from Gin context (set by `AuthMiddleware`)
    - Validate `:id` path param (400 if empty or whitespace-only)
    - Call `service.GetDetail` and map `ErrNotFound`→404, `ErrDeleted`→410, `ErrForbidden`→403, `ErrBillingParse`→500
    - Set `Content-Type: application/json; charset=utf-8`
    - Wrap result in `ResponseEnvelope{APIVersion: "1", Data: detail}` and respond with 200
  - _Requirements: 1.1, 1.2, 1.3, 3.2, 4.1, 4.2, 5.1, 6.1, 6.2_

  - [ ]\* 7.1 Write property test for handler — Property 7: Unknown subscription ID yields HTTP 404
    - **Property 7: Unknown subscription ID yields HTTP 404**
    - **Validates: Requirements 1.2**

  - [ ]\* 7.2 Write property test for handler — Property 8: Malformed subscription ID yields HTTP 400
    - **Property 8: Malformed subscription ID yields HTTP 400**
    - **Validates: Requirements 1.3**

  - [ ]\* 7.3 Write property test for handler — Property 10: Non-owner credential yields HTTP 403
    - **Property 10: Non-owner credential yields HTTP 403**
    - **Validates: Requirements 6.2**

  - [ ]\* 7.4 Write property test for handler — Property 11: No sensitive fields in response
    - **Property 11: No sensitive fields in response**
    - **Validates: Requirements 6.3**

  - [ ]\* 7.5 Write property test for handler — Property 12: JSON round-trip fidelity
    - **Property 12: JSON round-trip fidelity**
    - **Validates: Requirements 7.6**

  - [ ]\* 7.6 Write unit tests for GetSubscription handler
    - 401 on missing/invalid `Authorization` header
    - 403 on wrong caller
    - 400 on empty/malformed `:id`
    - 404 on unknown ID
    - 410 on soft-deleted subscription
    - 500 on unparseable amount
    - 200 with full envelope on happy path
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [x] 8. Wire service and middleware into routes
  - Update `internal/routes/` to apply `AuthMiddleware` to `GET /api/subscriptions/:id`
  - Inject `SubscriptionService` (with real or stub repositories) into the handler
  - Update `cmd/server/main.go` if needed to construct and wire dependencies
  - _Requirements: 1.1, 4.2, 6.1_

- [x] 9. Write integration test
  - Add integration test in `internal/handlers/subscriptions_test.go` using `httptest.NewRecorder`
  - Exercise `GET /api/subscriptions/:id` end-to-end with mock repositories
  - Assert full `ResponseEnvelope` shape: `api_version`, `data` fields, `Content-Type` header
  - _Requirements: 7.5_

- [x] 10. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP
- Property-based tests use `pgregory.net/rapid`; each test is tagged with `// Feature: subscription-detail-expansion, Property <N>: <text>`
- Each property test runs a minimum of 100 iterations (rapid default)
- `CustomerID` must never appear in any exported response type
