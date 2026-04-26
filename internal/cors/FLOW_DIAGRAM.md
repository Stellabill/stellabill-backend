# CORS Request Flow Diagram

## Overview

This document illustrates the request flow through the hardened CORS middleware.

## Request Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        Browser Request                          │
│                    (with Origin header)                         │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                     CORS Middleware Entry                       │
│                  Set Vary: Origin header                        │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
                    ┌────────────────┐
                    │ Origin header  │
                    │   present?     │
                    └────┬───────┬───┘
                         │       │
                    No   │       │   Yes
                         │       │
                         ▼       ▼
              ┌──────────────┐  ┌──────────────────────────────┐
              │ Skip CORS    │  │  Validate Origin Format      │
              │ (same-origin)│  │  - Has scheme?               │
              │ Pass through │  │  - Has host?                 │
              └──────────────┘  │  - No path/query/fragment?   │
                                └────────┬─────────────────────┘
                                         │
                                         ▼
                                ┌────────────────┐
                                │ Format valid?  │
                                └────┬───────┬───┘
                                     │       │
                                No   │       │   Yes
                                     │       │
                                     ▼       ▼
                          ┌──────────────┐  ┌──────────────────┐
                          │ Preflight?   │  │ Check Allowlist  │
                          └────┬─────┬───┘  │ - Wildcard?      │
                               │     │      │ - In list?       │
                          Yes  │     │  No  └────────┬─────────┘
                               │     │               │
                               ▼     ▼               ▼
                        ┌──────────┐ ┌──────────┐  ┌────────────────┐
                        │ Return   │ │ Pass     │  │ Origin allowed?│
                        │ 403      │ │ through  │  └────┬───────┬───┘
                        └──────────┘ └──────────┘       │       │
                                                    No   │       │   Yes
                                                         │       │
                                                         ▼       ▼
                                              ┌──────────────┐  ┌──────────────────┐
                                              │ Preflight?   │  │ Set CORS Headers │
                                              └────┬─────┬───┘  │ - ACAO           │
                                                   │     │      │ - ACAM           │
                                              Yes  │     │  No  │ - ACAH           │
                                                   │     │      │ - ACAC (if set)  │
                                                   ▼     ▼      │ - Max-Age        │
                                            ┌──────────┐ ┌──────┴──────────────────┘
                                            │ Return   │ │ Pass through            │
                                            │ 403      │ │ to handler              │
                                            └──────────┘ └─────────────────────────┘
                                                         │
                                                         ▼
                                              ┌──────────────────┐
                                              │ Preflight?       │
                                              └────┬─────────┬───┘
                                                   │         │
                                              Yes  │         │  No
                                                   │         │
                                                   ▼         ▼
                                            ┌──────────┐  ┌──────────┐
                                            │ Return   │  │ Continue │
                                            │ 204      │  │ to route │
                                            └──────────┘  └──────────┘
```

## Decision Points

### 1. Origin Header Present?

**No Origin Header**:
- Same-origin request or non-browser client
- Skip CORS processing
- Set Vary: Origin header
- Pass through to handler

**Origin Header Present**:
- Cross-origin request
- Proceed to validation

### 2. Origin Format Valid?

**Validation Checks**:
- Has scheme (http:// or https://)
- Has host
- No path component
- No query parameters
- No fragment

**Invalid Format**:
- Malformed origin
- If preflight: Return 403
- If regular request: Pass through without CORS headers

**Valid Format**:
- Proceed to allowlist check

### 3. Origin in Allowlist?

**Allowlist Check**:
- Wildcard profile: Allow all origins
- Production profile: Check exact match in list

**Not Allowed**:
- Origin not in allowlist
- If preflight: Return 403
- If regular request: Pass through without CORS headers

**Allowed**:
- Set CORS headers
- Continue processing

### 4. Preflight Request?

**Preflight (OPTIONS)**:
- Set all CORS headers
- Set Max-Age header
- Return 204 No Content
- Stop processing

**Regular Request**:
- Set CORS headers
- Continue to route handler

## CORS Headers Set

### For Wildcard Profile (Development)

```
Vary: Origin
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key
```

### For Production Profile (Allowlist)

```
Vary: Origin
Access-Control-Allow-Origin: https://app.stellarbill.com
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key
Access-Control-Allow-Credentials: true
Access-Control-Max-Age: 43200
```

## Example Scenarios

### Scenario 1: Valid Allowed Origin

```
Request:
  GET /api/v1/plans
  Origin: https://app.stellarbill.com

Flow:
  1. Origin present ✓
  2. Format valid ✓
  3. In allowlist ✓
  4. Not preflight
  5. Set CORS headers
  6. Continue to handler

Response:
  200 OK
  Vary: Origin
  Access-Control-Allow-Origin: https://app.stellarbill.com
  Access-Control-Allow-Credentials: true
```

### Scenario 2: Disallowed Origin

```
Request:
  GET /api/v1/plans
  Origin: https://evil.example.com

Flow:
  1. Origin present ✓
  2. Format valid ✓
  3. In allowlist ✗
  4. Not preflight
  5. No CORS headers
  6. Continue to handler

Response:
  200 OK
  Vary: Origin
  (No Access-Control-Allow-Origin header)
  
Browser: Blocks response due to CORS policy
```

### Scenario 3: Malformed Origin

```
Request:
  GET /api/v1/plans
  Origin: app.stellarbill.com

Flow:
  1. Origin present ✓
  2. Format valid ✗ (missing scheme)
  3. Not preflight
  4. No CORS headers
  5. Continue to handler

Response:
  200 OK
  Vary: Origin
  (No Access-Control-Allow-Origin header)
```

### Scenario 4: Preflight - Allowed Origin

```
Request:
  OPTIONS /api/v1/plans
  Origin: https://app.stellarbill.com
  Access-Control-Request-Method: POST

Flow:
  1. Origin present ✓
  2. Format valid ✓
  3. In allowlist ✓
  4. Is preflight ✓
  5. Set CORS headers
  6. Return 204

Response:
  204 No Content
  Vary: Origin
  Access-Control-Allow-Origin: https://app.stellarbill.com
  Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
  Access-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key
  Access-Control-Allow-Credentials: true
  Access-Control-Max-Age: 43200
```

### Scenario 5: Preflight - Disallowed Origin

```
Request:
  OPTIONS /api/v1/plans
  Origin: https://evil.example.com
  Access-Control-Request-Method: POST

Flow:
  1. Origin present ✓
  2. Format valid ✓
  3. In allowlist ✗
  4. Is preflight ✓
  5. Return 403

Response:
  403 Forbidden
  Vary: Origin
  (No CORS headers)
```

### Scenario 6: Same-Origin Request

```
Request:
  GET /api/v1/plans
  (No Origin header)

Flow:
  1. Origin present ✗
  2. Skip CORS
  3. Continue to handler

Response:
  200 OK
  Vary: Origin
  (No CORS headers needed)
```

## Security Checkpoints

### Checkpoint 1: Origin Header Validation
- **Purpose**: Detect malformed origins early
- **Action**: Reject invalid formats without CORS headers
- **Protection**: Prevents origin manipulation attacks

### Checkpoint 2: Allowlist Verification
- **Purpose**: Enforce explicit origin allowlist
- **Action**: Only allow configured origins
- **Protection**: Prevents unauthorized cross-origin access

### Checkpoint 3: Preflight Rejection
- **Purpose**: Fail fast for disallowed origins
- **Action**: Return 403 for preflight from disallowed origins
- **Protection**: Clear error signal to browser

### Checkpoint 4: Vary Header
- **Purpose**: Prevent cache poisoning
- **Action**: Always set Vary: Origin
- **Protection**: Ensures CDN/proxy caches per-origin

## Performance Considerations

### Caching
- Preflight responses cached for 12 hours (production)
- Reduces preflight round-trips
- Browser handles cache automatically

### Validation Cost
- Origin format validation: O(1)
- Allowlist lookup: O(n) where n is typically <10
- Minimal performance impact

### Early Exit
- Same-origin requests skip CORS processing
- Invalid origins rejected early
- Efficient request handling

## Monitoring Points

### Metrics to Track
1. **Preflight Requests**: Count of OPTIONS requests
2. **Rejected Origins**: Count of 403 responses
3. **Malformed Origins**: Count of format validation failures
4. **Allowed Origins**: Count of successful CORS requests

### Alert Conditions
1. **High Rejection Rate**: May indicate attack or misconfiguration
2. **Validation Failures**: Configuration issues
3. **Wildcard in Production**: Critical security violation
