# PR Description

## Description

This PR implements tracking for failed admin authentication attempts per source and per account, utilizing an exponential backoff strategy that doubles the lockout duration (from 1 second up to 15 minutes) with each consecutive failure. It also introduces metrics and audit logs for monitoring and alerting.

## Changes Made

- **`internal/security/lockout.go`**: Introduced `LockoutTracker` with thread-safe support for tracking failures and lockouts per `source` + `account` pair. Added an exported `admin_login_lockouts_total` Prometheus metric.
- **`internal/security/lockout_test.go`**: Comprehensive test coverage checking clock skew handling, race conditions, edge cases, and ensuring that expiration logic handles delays properly (clock skew across replicas does not double-count).
- **`internal/handlers/admin.go`**: Updated `AdminHandler.Login` to use `LockoutTracker`. Used constant-time comparison for validating token credentials.
- **Audit Logging**: Emits audit logs with `action="admin_login"`, detailing lockouts, credential rejections, and successes.

## Reset Procedure

- Lockouts are stored in-memory per replica.
- A successful login immediately resets the lockout for that specific account and source IP pair.
- There is intentionally no exposed admin API to forcefully clear lockouts to prevent attackers from suppressing rate-limit alerts with compromised credentials. Operators can reset lockouts globally by restarting the server instances. 

## Testing and Verification

- The new security package is fully tested (`go test ./internal/security/... ./internal/handlers/...`) and exceeds the 95% test coverage requirement. 
- The implementation strictly relies on UTC-normalized timestamps for handling clock drift between replicas efficiently.
