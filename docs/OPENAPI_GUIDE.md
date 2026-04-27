# OpenAPI Contract Guide

## Spec-First Policy

Stellabill Backend follows a **spec-first** approach: any API change must be reflected in the OpenAPI specification before implementation. This ensures the API contract is always documented and validated.

## Contributor Checklist for API Changes

When adding or modifying API endpoints, follow this checklist:

### 1. Update OpenAPI Specification
- [ ] Add or update the path in `openapi/openapi.yaml`
- [ ] Define all request parameters (path, query, header)
- [ ] Define request body schema for POST/PUT/PATCH
- [ ] Define response schemas for all status codes
- [ ] Add security requirements if authentication is needed
- [ ] Update the `operationId` to be unique
- [ ] Add appropriate tags

### 2. Implement the Endpoint
- [ ] Implement the handler in `internal/handlers/`
- [ ] Register the route in `internal/routes/routes.go` (only once!)
- [ ] Ensure consistent API versioning (use `/api/v1/` prefix for versioned endpoints)
- [ ] Add authentication/authorization as specified in the OpenAPI security scheme

### 3. Validate Contract
- [ ] Run `go test ./internal/contract/...` to verify the endpoint matches the spec
- [ ] Run `go run ./cmd/openapi-validate` to check for discrepancies
- [ ] Ensure CI passes (contract tests are run automatically)

### 4. Documentation
- [ ] Update README.md if the endpoint changes public API surface
- [ ] Add inline documentation for complex logic

## Versioning Strategy

- **Versioned endpoints**: All endpoints that require authentication should be under `/api/v1/` prefix.
- **Unversioned endpoints**: Only public endpoints like health check may remain under `/api/` without version.
- **Backward compatibility**: When making changes to existing endpoints:
  - Non-breaking changes (adding optional fields) can be done in the same version.
  - Breaking changes (removing fields, changing types) require a new version (`/api/v2/`).
- **Deprecation**: Mark old versions as deprecated in the OpenAPI spec using `deprecated: true`.

## Security Considerations

- All versioned endpoints must have security defined in the OpenAPI spec.
- Use Bearer token (JWT) authentication as defined in `securitySchemes`.
- Ensure sensitive data is not exposed in responses (check the OpenAPI spec).
- Validate that error responses don't leak sensitive information.

## Common Mistakes to Avoid

1. **Duplicate route registration**: Each endpoint should be registered exactly once in `routes.go`.
2. **Missing security**: Forgetting to add `security:` to the OpenAPI operation.
3. **Inconsistent paths**: Using `/api/` for some endpoints and `/api/v1/` for others without reason.
4. **Skipping contract tests**: Always run contract tests after API changes.

## Running Validation Locally

```bash
# Validate OpenAPI spec can be loaded
go run ./cmd/openapi-validate

# Run contract tests
go test ./internal/contract/... -v

# Run all tests with coverage
go test ./... -cover
```

## CI Enforcement

The CI pipeline automatically:
- Runs contract tests (`go test ./...`)
- Validates OpenAPI spec (`go run ./cmd/openapi-validate`)
- Fails if any endpoint is not documented or if the implementation doesn't match the spec.

If CI fails due to OpenAPI contract issues, check:
1. Is the new endpoint added to `openapi/openapi.yaml`?
2. Does the implementation match the spec?
3. Are there duplicate route registrations?
