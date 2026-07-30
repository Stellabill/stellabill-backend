# @stellabill/sdk

> **Typed TypeScript SDK for the Stellabill backend API.**
> Generated from [`openapi/openapi.yaml`](../../openapi/openapi.yaml) using
> [`openapi-typescript`](https://openapi-ts.dev) + [`openapi-fetch`](https://openapi-ts.dev/openapi-fetch/).
> Published to npm on every GitHub release by the
> [release workflow](../../.github/workflows/release.yml).

[![npm version](https://img.shields.io/npm/v/@stellabill/sdk)](https://www.npmjs.com/package/@stellabill/sdk)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## Table of contents

- [Install](#install)
- [Requirements](#requirements)
- [Quickstart](#quickstart)
- [API surface](#api-surface)
- [Error handling](#error-handling)
- [Authentication & token rotation](#authentication--token-rotation)
- [Custom headers, middleware & fetch](#custom-headers-middleware--fetch)
- [Type-safe raw client](#type-safe-raw-client)
- [Versioning](#versioning)
- [Security assumptions](#security-assumptions)
- [Regenerating from OpenAPI](#regenerating-from-openapi)
- [Contributing](#contributing)
- [License](#license)

---

## Install

```sh
pnpm add @stellabill/sdk
# or
npm install @stellabill/sdk
# or
yarn add @stellabill/sdk
```

## Requirements

- **Node.js ≥ 20** (the SDK relies on the global `fetch` API). For older
  runtimes pass your own `fetch` (e.g. via `undici`) via
  `createStellarBillClient({ fetch })`.
- **TypeScript ≥ 5.0** (the SDK is published with both ESM and CJS bundles
  plus full `.d.ts` typings).

## Quickstart

```ts
import { createStellarBillClient } from '@stellabill/sdk';

const sdk = createStellarBillClient({
  baseUrl: 'https://api.stellabill.com',
  token: process.env.STELLABILL_TOKEN, // optional; required for protected routes
});

const { data, error, status } = await sdk.getHealth();
if (data) {
  console.log(`Service ${data.service} is ${data.status}`);
}
```

## API surface

The SDK exposes ergonomic, typed wrappers for every operation documented in
`openapi/openapi.yaml`. Each wrapper returns a `Promise<SdkResult<T>>` with
this shape:

```ts
interface SdkResult<T> {
  data: T | undefined;            // parsed 2xx body
  error: ApiErrorBody | undefined; // parsed non-2xx body (typed envelope)
  status: number;                  // HTTP status code
  response: Response;              // original Response (headers, etc.)
  requestMethod: string;           // e.g. "GET"
  requestUrl: string;              // final resolved URL
}
```

| Method                                          | OpenAPI operation           | Path                                |
| ----------------------------------------------- | --------------------------- | ----------------------------------- |
| `sdk.getHealth()`                               | `getHealth`                 | `GET /api/health`                   |
| `sdk.listPlans({ cursor?, limit? })`            | `listPlans`                 | `GET /api/v1/plans`                 |
| `sdk.listSubscriptions({ cursor?, limit? })`    | `listSubscriptions`         | `GET /api/subscriptions`            |
| `sdk.getSubscription(id)`                       | `getSubscriptionV1`         | `GET /api/subscriptions/{id}`       |
| `sdk.inspectIdempotencyKey(key)`                | `inspectIdempotencyKey`     | `GET /api/v1/idempotency/{key}`     |

## Error handling

By default, the SDK **does not throw** on non-2xx responses — it returns a
typed `{ status, error, data, response }` so you decide how to handle it.

For a more conventional throw-on-error flow, enable `throwOnError: true`:

```ts
import { createStellarBillClient, StellarBillError } from '@stellabill/sdk';

const sdk = createStellarBillClient({
  baseUrl: 'https://api.stellabill.com',
  token: process.env.STELLABILL_TOKEN!,
  throwOnError: true,
});

try {
  const sub = await sdk.getSubscription('sub_xyz');
  console.log(sub.data?.status);
} catch (err) {
  if (err instanceof StellarBillError) {
    // err.status, err.body.code, err.requestUrl, err.requestMethod
    logger.warn(`API error ${err.status} on ${err.requestUrl}: ${err.body?.code}`);
  } else {
    throw err; // programmer error (network, bad config, etc.)
  }
}
```

Or, per-call, use `assertOk(result)` to throw on a single result.

## Authentication & token rotation

Pass a bearer token at construction time. The SDK attaches it as
`Authorization: Bearer <token>` on every request.

```ts
const sdk = createStellarBillClient({
  baseUrl: 'https://api.stellabill.com',
  token: initialToken,
});

// Later, after a refresh:
sdk.setToken(newToken);
// Or clear it:
sdk.setToken(undefined);
```

## Custom headers, middleware & fetch

The SDK passes through arbitrary caller headers but **strips any caller-supplied
`Authorization` header** — the bearer token configured via `token` /
`setToken()` is the single source of truth for auth, eliminating smuggled
or shadow auth.

```ts
const sdk = createStellarBillClient({
  baseUrl: 'https://api.stellabill.com',
  token: process.env.STELLABILL_TOKEN!,
  headers: { 'X-Trace-Id': crypto.randomUUID() }, // arbitrary caller headers
  middleware: [
    {
      async onRequest({ request }) {
        request.headers.set('X-Trace-Id', crypto.randomUUID());
        return request;
      },
    },
  ],
  fetch: undiciFetch, // optional, e.g. for Bun / older Node
});
```

## Type-safe raw client

The underlying `openapi-fetch` client is exposed as `sdk.raw`, so advanced
callers can issue any operation the OpenAPI spec defines and get full type
inference:

```ts
import type { components } from '@stellabill/sdk';

type Plan = components['schemas']['Plan'];

const { data } = await sdk.raw.GET('/api/v1/plans', { params: { query: { limit: 5 } } });
//    ^? { plans?: Plan[]; pagination: ...; } | undefined
```

## Versioning

- The SDK's `package.json` version is bumped by the
  [release workflow](../../.github/workflows/release.yml) on every GitHub
  release. It tracks `info.version` in `openapi/openapi.yaml`.
- API semver: breaking changes to the OpenAPI spec require a major SDK
  bump. Additive changes (new optional fields, new endpoints, new headers)
  ship as a minor bump. Bug fixes that don't change types ship as patch.
- The release workflow also runs the full test + coverage gate (≥95%)
  before publishing, so a broken release is impossible.
- **Provenance:** `publishConfig.provenance: true` is set, but provenance
  is only generated when OIDC *trusted publishing* is configured at
  [npmjs.com](https://docs.npmjs.com/generating-provenance-statements).
  Until then, publishing falls back to legacy `NPM_TOKEN` auth without
  provenance.

## Security assumptions

- **HTTPS in production.** The SDK warns to `console.warn` when given an
  `http://` baseUrl outside `localhost`. Pass `https://...` for any
  non-local environment.
- **Token integrity is the caller's responsibility.** The SDK trims and
  rejects tokens with embedded whitespace, but does not otherwise inspect
  or store them. Rotate tokens via `setToken()` after refresh; do not
  log raw tokens.
- **No secrets in URL paths.** `getSubscription` and
  `inspectIdempotencyKey` URL-encode their path segment via
  `encodeURIComponent`, so slashes/spaces in IDs are persisted correctly
  without escaping failures. Avoid putting tokens in URLs entirely.
- **User-Agent is set automatically.** Format:
  `@stellabill/sdk/<version> node/<node-version>`. The header is
  lower-case and stub-resilient: it is always present on every request.
- **Generated types are committed** (see `src/types/api.gen.ts`) so the
  the SDK ships zero build dependencies beyond `openapi-fetch` and is
  installable on locked-down networks.

## Regenerating from OpenAPI

```sh
cd sdks/ts
pnpm install
pnpm generate    # writes src/types/api.gen.ts from ../../openapi/openapi.yaml
pnpm build       # emits dist/*.js + dist/*.d.ts via tsup
pnpm test        # vitest run + coverage >= 95%
```

The generator reads
[`openapi/openapi.yaml`](../../openapi/openapi.yaml) and writes a committed
generated file at `src/types/api.gen.ts`. Do **not** edit it by hand —
update the spec instead.

## Contributing

For SDK-specific changes:

1. Update the spec in [`openapi/openapi.yaml`](../../openapi/openapi.yaml).
2. Run `pnpm generate` to refresh generated types (commit the diff).
3. Add or update tests in `test/` covering the change (the 95% coverage
   gate will fail otherwise).
4. Open a PR. CI runs `pnpm test` with the coverage threshold check.

For repo-wide guidelines see the existing top-level docs.

## License

[MIT](../../LICENSE) © Stellabill contributors.
