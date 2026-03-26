# Authorization and Authentication Documentation

## Overview

This document describes the endpoint-level authorization system for Stellarbill-backend. All endpoints enforce authentication and authorization based on user roles and route requirements.

## Authentication

### JWT Token Format

Tokens are signed with HS256 using the `JWT_SECRET` environment variable.

**Claims Structure:**
```json
{
  "user_id": "string",
  "email": "string",
  "role": "string",
  "roles": ["string"],
  "merchant_id": "string",
  "exp": "timestamp",
  "iat": "timestamp",
  "nbf": "timestamp"
}
```

### Token Validation

- All protected endpoints require a valid JWT token in the `Authorization` header
- Format: `Authorization: Bearer <token>`
- Token signature must be valid (HS256 with configured JWT_SECRET)
- Token must not be expired
- Token must contain required claims (`user_id`)

### Authentication Errors

| Status | Error | Cause |
|--------|-------|-------|
| 401 | `missing authorization header` | No Authorization header provided |
| 401 | `invalid authorization header format` | Header doesn't follow "Bearer <token>" format |
| 401 | `invalid or expired token` | Token signature invalid or expired |
| 401 | `invalid token claims: missing user_id` | Token missing required user_id claim |

## Authorization

### Role-Based Access Control (RBAC)

Three roles are defined:
- **admin**: Full access to all endpoints
- **merchant**: Access to merchant-specific endpoints
- **customer**: Limited access to customer-facing endpoints

### Route Authorization Matrix

| Method | Path | Public | Required Roles | Description |
|--------|------|--------|----------------|-------------|
| GET | `/api/health` | Yes | - | Service health check |
| GET | `/api/plans` | No | - (any authenticated) | List plans |
| GET | `/api/subscriptions` | No | admin, merchant | List subscriptions |
| GET | `/api/subscriptions/:id` | No | admin, merchant | Get subscription by ID |

### Authorization Errors

| Status | Error | Cause |
|--------|-------|-------|
| 403 | `insufficient permissions` | User's roles don't match required roles |

## Implementation Details

### Middleware Chain

Protected routes use the following middleware chain:

1. **corsMiddleware()** - Handles CORS headers (global)
2. **AuthMiddleware** - Validates JWT token (route group)
3. **AuthzMiddleware** - Checks required roles (individual routes)

### Middleware Configuration

**AuthMiddleware**
```go
authenticated := api.Group("")
authenticated.Use(auth.AuthMiddleware(cfg.JWTSecret))
```

Validates JWT signature and required claims.

**AuthzMiddleware**
```go
authenticated.GET("/subscriptions", 
  auth.AuthzMiddleware(auth.RoleAdmin, auth.RoleMerchant), 
  handlers.ListSubscriptions)
```

Checks if user has any of the specified roles. If no roles are specified, any authenticated user is allowed.

## Testing

### Test Coverage

Comprehensive endpoint tests verify:

1. **Authentication Tests**
   - Missing token (401)
   - Malformed header (401)
   - Expired token (401)
   - Invalid signature (401)
   - Token without required claims (401)
   - Valid token (200)

2. **Authorization Tests**
   - Insufficient permissions (403)
   - Authorized role access (200)
   - Unauthorized role access (403)

3. **Edge Cases**
   - Malformed JWT structure
   - Token without user_id claim
   - Token without roles
   - Token with wrong signing algorithm

### Running Tests

```bash
# Run all tests
go test ./...

# Run authorization tests with verbose output
go test -v ./internal/handlers/authorization_test.go -test.v

# Run specific test
go test -run TestListSubscriptionsAuthorization ./...
```

### Test Scenarios

Each endpoint is tested with:
- No token
- Expired token
- Invalid signature
- Valid admin token
- Valid merchant token
- Valid customer token (where applicable)
- Token without required claims

## Security Considerations

### Token Storage

- JWT tokens should be stored securely (HttpOnly cookies or secure storage)
- Never expose tokens in logs or error messages
- Always use HTTPS in production

### Claims Validation

- `user_id` claim is required for all tokens
- Role claim should be validated per endpoint
- Expired tokens are automatically rejected

### Rate Limiting

Future implementations should add:
- Rate limiting per user
- Token refresh mechanisms
- API key authentication for machine-to-machine communication

## Future Extensions

1. **API Key Authentication** - For service-to-service calls
2. **Role-Based Resource Filtering** - Filter subscriptions by merchant_id
3. **Fine-Grained Permissions** - More granular than role-based (create, read, update, delete)
4. **Token Refresh** - Implement refresh token flow
5. **Audit Logging** - Log all authentication/authorization events
6. **Multi-Tenancy** - Proper merchant isolation using merchant_id

## Examples

### Making Authenticated Requests

**With valid token:**
```bash
curl -H "Authorization: Bearer <token>" \
  https://api.stellarbill.io/api/subscriptions
```

**Response (200 OK):**
```json
{
  "subscriptions": [...]
}
```

**Invalid/missing token:**
```bash
curl https://api.stellarbill.io/api/subscriptions
```

**Response (401 Unauthorized):**
```json
{
  "error": "missing authorization header"
}
```

**Insufficient permissions:**
```bash
# Customer token trying to access merchant-only endpoint
curl -H "Authorization: Bearer <customer_token>" \
  https://api.stellarbill.io/api/subscriptions
```

**Response (403 Forbidden):**
```json
{
  "error": "insufficient permissions"
}
```

## Contact & Support

For questions about authorization or authentication, see the inline code documentation or contact the backend team.
