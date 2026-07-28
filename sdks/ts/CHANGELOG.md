# Changelog

All notable changes to `@stellabill/sdk` are recorded here. Versions follow
[Semantic Versioning](https://semver.org/) and track
`openapi/openapi.yaml`'s `info.version`.

## 0.2.0 (initial release)

- Initial SDK release. Generated from `openapi/openapi.yaml` v0.2.0 using
  [`openapi-typescript`](https://openapi-ts.dev) + [`openapi-fetch`](https://openapi-ts.dev/openapi-fetch/).
- Typed wrappers for `/api/health`, `/api/v1/plans`, `/api/subscriptions`,
  `/api/subscriptions/{id}`, `/api/v1/idempotency/{key}`.
- Bearer token authentication with runtime rotation via `setToken()`.
- Opt-in `throwOnError` semantics + `assertOk()` helper.
- Committed generated types (`src/types/api.gen.ts`) so IDEs work without a
  build step.
- ≥95% test coverage; round-trip tests guarantee generated types match
  runtime responses.
