/**
 * @stellabill/sdk — typed TypeScript client for the Stellabill backend API.
 *
 * Generated from `openapi/openapi.yaml` via `openapi-typescript` + `openapi-fetch`.
 * Install with:
 *
 * ```sh
 * pnpm add @stellabill/sdk
 * ```
 *
 * See [`README.md`](../../README.md) for the full API surface and examples.
 */

export {
  createStellarBillClient,
  assertOk,
  safeParseErrorBody,
  type StellarBillClient,
  type StellarBillClientOptions,
  type SdkResult,
  type HealthResult,
  type ListPlansResult,
  type ListSubscriptionsResult,
  type GetSubscriptionResult,
  type InspectIdempotencyKeyResult,
  type ListPlansParams,
  type ListSubscriptionsParams,
  type FetchLike,
} from './client.js';

export {
  StellarBillError,
  StellarBillConfigError,
  type ApiErrorBody,
} from './errors.js';

export { TokenHolder, sanitizeToken } from './auth.js';

export { SDK_VERSION, defaultUserAgent } from './version.js';

// Generated types are re-exported so consumers can `import type { components }`
// from a stable entrypoint.
export type { paths, components, operations } from './types/api.gen.js';
