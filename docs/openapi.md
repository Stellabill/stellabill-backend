# OpenAPI specification

The API contract for this service lives in `openapi/openapi.yaml`.

## Policy (versioning + changes)

- The OpenAPI `info.version` uses semver.
- **Breaking changes** (remove/rename fields, change types, change required fields, remove endpoints, change auth semantics) require a **major** bump.
- **Additive changes** (new optional fields, new endpoints, new 2xx responses that do not break clients) require a **minor** bump.
- **Documentation-only fixes** (typos, descriptions, examples) require a **patch** bump.

## Keeping implementation and spec in sync

The test suite includes spec validation and contract checks:

```bash
go test ./...
```

What the checks do:

- Validate that the OpenAPI document is syntactically and semantically correct.
- Ensure every implemented `/api/*` route is present in the OpenAPI document.
- Validate real HTTP responses against the OpenAPI response schemas (drift prevention).

## Updating the contract

When you change a handler, update `openapi/openapi.yaml` in the same PR and keep the versioning policy above.

