# API Error Envelope Standardization

## Overview

This document describes the standardized error response envelope used across all API endpoints in the Stellabill backend. This ensures consistent error handling, improved observability, and better client error handling.

## Error Response Format

All error responses follow a standardized JSON envelope structure:

```json
{
  "code": "ERROR_CODE",
  "message": "Human-readable error message",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "details": {
    "field": "optional",
    "reason": "additional context"
  }
}
```

### Fields

- **code** (string, required): Machine-readable error code for programmatic error handling
  - Examples: `NOT_FOUND`, `UNAUTHORIZED`, `VALIDATION_FAILED`, `INTERNAL_ERROR`
- **message** (string, required): Human-readable error description
- **trace_id** (string, required): Unique identifier for this request, used for logging and debugging
  - Format: UUID v4
  - Persisted in response headers and logs for request tracking
- **details** (object, optional): Additional context-specific information
  - Used for validation errors to indicate which field failed and why

## Error Codes

### Client Errors (4xx)

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `BAD_REQUEST` | 400 | Invalid request parameters or format |
| `VALIDATION_FAILED` | 400 | Input validation failed (detailed in `details`) |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication credentials |
| `FORBIDDEN` | 403 | Authenticated user lacks permission for resource |
| `NOT_FOUND` | 404 | Requested resource does not exist |
| `CONFLICT` | 409 | Request conflicts with current resource state |

### Server Errors (5xx)

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INTERNAL_ERROR` | 500 | Unexpected server error |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily unavailable |

## Examples

### Not Found Error

```bash
$ curl -H "Authorization: Bearer <token>" \
       -H "X-Tenant-ID: tenant-1" \
       http://localhost:8080/api/subscriptions/nonexistent

HTTP/1.1 404 Not Found
X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000

{
  "code": "NOT_FOUND",
  "message": "The requested resource was not found",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Validation Error

```bash
$ curl -H "Authorization: Bearer <token>" \
       -H "X-Tenant-ID: tenant-1" \
       http://localhost:8080/api/subscriptions/

HTTP/1.1 400 Bad Request
X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000

{
  "code": "VALIDATION_FAILED",
  "message": "subscription id is required",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "details": {
    "field": "id",
    "reason": "cannot be empty"
  }
}
```

### Unauthorized Error

```bash
$ curl http://localhost:8080/api/subscriptions/sub-123

HTTP/1.1 401 Unauthorized
X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000

{
  "code": "UNAUTHORIZED",
  "message": "authorization header required",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

## Trace ID Tracking

Every request is assigned a unique trace ID for request tracking and debugging:

1. If client provides `X-Trace-ID` header, that value is used
2. Otherwise, a new UUID is generated
3. Trace ID is available in:
   - Context (`c.GetString("traceID")`)
   - Response body (`error.trace_id`)
   - Response headers (`X-Trace-ID`)
   - Application logs (for integration with observability tools)

This allows correlating client requests with server logs and metrics.

## Implementation Details

### Error Mapping

Service layer errors are automatically mapped to HTTP status codes and error codes:

```go
// maps service.ErrNotFound → 404 NOT_FOUND
// maps service.ErrForbidden → 403 FORBIDDEN
// maps service.ErrDeleted → 410 Gone with NOT_FOUND code
// maps service.ErrBillingParse → 500 INTERNAL_ERROR
```

### Centralized Error Helpers

All error responses use helper functions in `internal/handlers/errors.go`:

```go
// Generic error response
RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, "Not found")

// Error with additional details
RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, 
  "Invalid input", map[string]interface{}{
    "field": "email",
    "reason": "invalid format",
  })

// Specialized helpers
RespondWithAuthError(c, "Missing authentication credentials")
RespondWithValidationError(c, "Field validation failed", details)
RespondWithNotFoundError(c, "Subscription")
RespondWithInternalError(c, "Database connection failed")
```

### Handler Implementation Pattern

All handlers should follow this pattern:

```go
func MyHandler(c *gin.Context) {
  // 1. Validate authentication
  callerID, exists := c.Get("callerID")
  if !exists {
    RespondWithAuthError(c, "Missing authentication credentials")
    return
  }

  // 2. Validate input
  if err := validateInput(c); err != nil {
    RespondWithValidationError(c, err.Error(), details)
    return
  }

  // 3. Call business logic
  result, err := service.DoSomething(c.Request.Context())
  if err != nil {
    statusCode, code, message := MapServiceErrorToResponse(err)
    RespondWithError(c, statusCode, code, message)
    return
  }

  // 4. Return success
  c.JSON(http.StatusOK, result)
}
```

## Client Implementation Guide

### Error Handling Pattern

Clients should handle errors using the standardized error code:

#### JavaScript/TypeScript Example

```typescript
interface ApiError {
  code: string;
  message: string;
  trace_id: string;
  details?: Record<string, any>;
}

async function fetchSubscription(id: string) {
  try {
    const response = await fetch(`/api/subscriptions/${id}`, {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    
    if (!response.ok) {
      const error: ApiError = await response.json();
      
      switch (error.code) {
        case 'NOT_FOUND':
          console.error('Subscription not found');
          break;
        case 'UNAUTHORIZED':
          // Refresh token or redirect to login
          redirectToLogin();
          break;
        case 'VALIDATION_FAILED':
          // Show field-specific errors from details
          showValidationErrors(error.details);
          break;
        case 'INTERNAL_ERROR':
          console.error('Server error, trace ID:', error.trace_id);
          break;
        default:
          console.error('Unknown error:', error);
      }
    }
    
    return response.json();
  } catch (err) {
    console.error('Network error:', err);
    throw err;
  }
}
```

#### Python Example

```python
import requests
from typing import Optional, Dict, Any

class ApiError(Exception):
    def __init__(self, code: str, message: str, trace_id: str, details: Optional[Dict] = None):
        self.code = code
        self.message = message
        self.trace_id = trace_id
        self.details = details or {}

def fetch_subscription(subscription_id: str, token: str) -> Dict:
    response = requests.get(
        f'http://api.example.com/api/subscriptions/{subscription_id}',
        headers={'Authorization': f'Bearer {token}'}
    )
    
    if not response.ok:
        error_data = response.json()
        raise ApiError(
            code=error_data['code'],
            message=error_data['message'],
            trace_id=error_data['trace_id'],
            details=error_data.get('details')
        )
    
    return response.json()

# Usage
try:
    sub = fetch_subscription('sub-123', token)
except ApiError as e:
    if e.code == 'NOT_FOUND':
        print(f"Subscription not found (trace: {e.trace_id})")
    elif e.code == 'VALIDATION_FAILED':
        print(f"Invalid input: {e.details}")
    elif e.code == 'UNAUTHORIZED':
        # Refresh token
        pass
```

### Trace ID Usage

Always log the trace ID when errors occur to enable debugging:

```typescript
// Store trace ID for support requests
localStorage.setItem('lastErrorTraceId', error.trace_id);

// Include in error reports
reportError({
  message: error.message,
  traceId: error.trace_id,
  timestamp: new Date().toISOString()
});
```

### Retry Strategy

Implement retry logic based on error codes:

```typescript
async function fetchWithRetry(
  url: string,
  maxRetries: number = 3
): Promise<any> {
  let lastError: ApiError | null = null;

  for (let i = 0; i < maxRetries; i++) {
    try {
      return await fetch(url);
    } catch (err) {
      lastError = err as ApiError;

      // Don't retry client errors (except 409 CONFLICT)
      if (lastError.code !== 'CONFLICT' && 
          lastError.code !== 'SERVICE_UNAVAILABLE') {
        throw err;
      }

      // Exponential backoff
      const delay = Math.pow(2, i) * 1000;
      await new Promise(resolve => setTimeout(resolve, delay));
    }
  }

  throw lastError;
}
```

## Security Considerations

### Sensitive Information

- **Never** expose internal error details to clients (e.g., stack traces, SQL queries)
- Use generic error messages for security-sensitive operations
- Detailed errors are logged server-side (with trace ID for debugging)
- Validation error details are safe to expose as they indicate user input issues

### Error Response Headers

- Trace ID is exposed in `X-Trace-ID` header for logging and support
- Clients should store this for error reports
- Rate limiting headers (if applicable) should be in response

### Authentication Errors

- Return `UNAUTHORIZED` (401) for missing/invalid credentials
- Return `FORBIDDEN` (403) for insufficient permissions
- Do NOT reveal whether a user exists or not

## Testing Error Responses

### Unit Test Example

```go
func TestErrorResponse(t *testing.T) {
  r := gin.New()
  r.Use(func(c *gin.Context) {
    c.Set("traceID", "test-trace-123")
  })
  
  r.GET("/test", func(c *gin.Context) {
    RespondWithError(c, http.StatusNotFound, 
      ErrorCodeNotFound, "Resource not found")
  })

  w := httptest.NewRecorder()
  req := httptest.NewRequest(http.MethodGet, "/test", nil)
  r.ServeHTTP(w, req)

  var env ErrorEnvelope
  json.Unmarshal(w.Body.Bytes(), &env)

  assert.Equal(t, http.StatusNotFound, w.Code)
  assert.Equal(t, "NOT_FOUND", env.Code)
  assert.Equal(t, "test-trace-123", env.TraceID)
}
```

## Debugging with Trace IDs

When a client reports an issue:

1. Get the trace ID from the error response
2. Search your application logs for that trace ID
3. Correlate with metrics and distributed tracing data
4. Example: `grep "trace-id=550e8400-e29b-41d4-a716-446655440000" app.log`

All error responses include the trace ID for full request traceability.

// Convenience helpers
RespondWithAuthError(c, "Missing auth header")
RespondWithNotFoundError(c, "subscription")
RespondWithValidationError(c, "Invalid input", details)
```

### Middleware Integration

The `TraceIDMiddleware` in `internal/middleware/traceid.go`:
- Injects trace ID into request context
- Sets `X-Trace-ID` response header
- Uses provided trace ID from client or generates a new UUID

Register middleware in routes:
```go
r.Use(middleware.TraceIDMiddleware())
```

## Security Considerations

1. **Error Messages**: Error messages are generic for security errors to avoid information disclosure
   - Bad authentication returns: "invalid or expired token" (not which part failed)
   - Permission denied returns: "forbidden" (not why)

2. **Trace IDs**: 
   - Used for audit logging and debugging
   - Never expose sensitive data in trace ID values
   - Trace IDs are UUIDs and don't contain information

3. **Details Field**:
   - Only use for validation errors with safe information
   - Never include passwords, tokens, or sensitive data
   - Example: `{"field": "email", "reason": "invalid format"}` ✅
   - Never: `{"field": "password", "received": "hunter2"}` ❌

## Testing Error Responses

Error handling is comprehensively tested in:
- `internal/handlers/errors_test.go` - Error envelope format and mapping
- `internal/handlers/subscriptions_test.go` - Integration with subscription handler
- `internal/middleware/traceid_test.go` - Trace ID generation and tracking

Test coverage includes:
- All error codes and HTTP status mappings
- Validation errors with details
- Authentication and authorization errors
- Trace ID generation and propagation
- Content-type headers
- Response envelope structure
