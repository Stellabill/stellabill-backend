import createClient, { type Middleware } from 'openapi-fetch';

import type { paths } from './types/api.gen.js';
import { TokenHolder, sanitizeToken } from './auth.js';
import { StellarBillConfigError, StellarBillError, type ApiErrorBody } from './errors.js';
import { SDK_VERSION, defaultUserAgent } from './version.js';

export type FetchLike = (input: RequestInfo, init?: RequestInit) => Promise<Response>;

export interface StellarBillClientOptions {
  /** API base URL (e.g. `https://api.stellabill.com`). */
  baseUrl: string;
  /** Initial bearer token; rotatable via `client.setToken()`. */
  token?: string;
  /** Optional pre-configured fetch implementation (Node 20+ has a built-in). */
  fetch?: FetchLike;
  /** Extra headers injected on every request (e.g. `X-Tenant-ID`, trace IDs). */
  headers?: Record<string, string>;
  /** Extra middleware runs after the SDK-installed auth + user-agent middleware. */
  middleware?: Middleware[];
  /** When `true`, throw {@link StellarBillError} on any non-2xx response. Default `false`. */
  throwOnError?: boolean;
}

export interface StellarBillClient {
  /** Underlying `openapi-fetch` client with full generated typings. */
  raw: ReturnType<typeof createClient<paths>>;
  /** Replace the bearer token used for authentication. */
  setToken(token: string | undefined): void;
  /** Read the currently configured bearer token (if any). */
  getToken(): string | undefined;
  /** Current SDK version. */
  version: string;

  // --- Typed operation wrappers (mirror openapi.yaml operations) ---
  getHealth(): Promise<HealthResult>;
  listPlans(params?: ListPlansParams): Promise<ListPlansResult>;
  listSubscriptions(params?: ListSubscriptionsParams): Promise<ListSubscriptionsResult>;
  getSubscription(id: string): Promise<GetSubscriptionResult>;
  inspectIdempotencyKey(key: string): Promise<InspectIdempotencyKeyResult>;
}

// ----- Typed result envelopes -----

export interface SdkResult<TData, TError = ApiErrorBody | undefined> {
  data: TData | undefined;
  /** HTTP status code (always present). */
  status: number;
  /** Parsed error body for non-2xx, else undefined. */
  error: TError;
  /** The original `Response` so callers can read response headers. */
  response: Response;
  /** HTTP method that produced this result. */
  requestMethod: string;
  /** Final resolved URL (after openapi-fetch substitution). */
  requestUrl: string;
}

export type HealthResult = SdkResult<import('./types/api.gen.js').components['schemas']['HealthResponse']>;
export type ListPlansResult = SdkResult<import('./types/api.gen.js').components['schemas']['PlansResponse']>;
export type ListSubscriptionsResult = SdkResult<
  import('./types/api.gen.js').components['schemas']['SubscriptionsResponse']
>;
export type GetSubscriptionResult = SdkResult<
  import('./types/api.gen.js').components['schemas']['Subscription']
>;
export type InspectIdempotencyKeyResult = SdkResult<
  import('./types/api.gen.js').components['schemas']['IdempotencyKeyRecord']
>;

export interface ListPlansParams {
  cursor?: string;
  limit?: number;
}
export interface ListSubscriptionsParams {
  cursor?: string;
  limit?: number;
}

// ----- Helpers -----

function validateBaseUrl(raw: unknown): string {
  if (raw === undefined || raw === null) {
    throw new StellarBillConfigError('baseUrl is required');
  }
  if (typeof raw !== 'string' || raw.trim().length === 0) {
    throw new StellarBillConfigError('baseUrl must be a non-empty string');
  }
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new StellarBillConfigError(`baseUrl "${raw}" is not a valid URL`);
  }
  // Strip trailing slashes for consistent URL composition.
  return parsed.toString().replace(/\/+$/, '');
}

function isLocalhost(baseUrl: string): boolean {
  try {
    const u = new URL(baseUrl);
    return u.hostname === 'localhost' || u.hostname === '127.0.0.1';
  } catch {
    return false;
  }
}

async function safeParseErrorBody(res: Response): Promise<ApiErrorBody | undefined> {
  const contentType = res.headers.get('content-type') ?? '';
  if (!contentType.includes('application/json')) return undefined;
  try {
    const text = await res.text();
    if (!text) return undefined;
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as ApiErrorBody;
    }
    return undefined;
  } catch {
    return undefined;
  }
}

function makeErrorMessage(method: string, url: string, status: number, body: ApiErrorBody | undefined): string {
  const detail = body?.message ?? body?.error ?? `HTTP ${status}`;
  return `${method} ${url} failed (${status}): ${detail}`;
}

/**
 * Build a fully-configured Stellabill SDK client bound to a single baseUrl.
 *
 * @example
 * ```ts
 * import { createStellarBillClient } from '@stellabill/sdk';
 *
 * const sdk = createStellarBillClient({
 *   baseUrl: 'https://api.stellabill.com',
 *   token: process.env.STELLABILL_TOKEN,
 * });
 *
 * const { data, error } = await sdk.getHealth();
 * if (error) throw new Error(error.message);
 * console.log(data?.status); // "ok"
 * ```
 */
export function createStellarBillClient(options: StellarBillClientOptions): StellarBillClient {
  const baseUrl = validateBaseUrl(options.baseUrl);

  if (typeof console !== 'undefined' && console.warn && baseUrl.startsWith('http://') && !isLocalhost(baseUrl)) {
    console.warn(`[stellabill-sdk] Insecure baseUrl "${baseUrl}" - use https:// in production`);
  }

  // Detect a real fetch (Node 20+ exposes globalThis.fetch; browsers always do).
  const providedFetch = options.fetch ?? (globalThis.fetch as FetchLike | undefined);
  if (typeof providedFetch !== 'function') {
    throw new StellarBillConfigError(
      'No fetch implementation available. Pass options.fetch (or run on Node >=20 / a modern browser).',
    );
  }

  const tokenHolder = new TokenHolder(sanitizeToken(options.token));
  const userAgent = defaultUserAgent();
  const extraHeaders: Record<string, string> = {};

  // Normalize caller headers and combine with User-Agent. Reject caller-
  // supplied Authorization headers so they cannot smuggle auth past the
  // SDK's token holder; the auth middleware below is the sole source of
  // truth for the `Authorization` header.
  if (options.headers) {
    for (const [k, v] of Object.entries(options.headers)) {
      const lower = k.toLowerCase();
      if (lower === 'authorization') continue;
      if (typeof v === 'string' && v.length > 0) {
        extraHeaders[lower] = v;
      }
    }
  }
  extraHeaders['user-agent'] = userAgent;

  const raw = createClient<paths>({
    baseUrl,
    fetch: providedFetch,
  });

  // Single middleware that injects user-agent, caller-supplied static
  // headers, and Authorization. Doing all three in middleware (instead
  // of split between createClient's `headers` option and middleware)
  // guarantees openapi-fetch has them on the Request before invoking
  // fetch, regardless of which request constructor it uses internally.
  const authMiddleware: Middleware = {
    async onRequest({ request }) {
      if (!request.headers.has('user-agent')) {
        request.headers.set('user-agent', userAgent);
      }
      for (const [k, v] of Object.entries(extraHeaders)) {
        if (!request.headers.has(k) && typeof v === 'string' && v.length > 0) {
          request.headers.set(k, v);
        }
      }
      if (tokenHolder.hasToken()) {
        const t = tokenHolder.get();
        if (t) request.headers.set('authorization', `Bearer ${t}`);
      }
      return request;
    },
  };
  raw.use(authMiddleware);
  if (options.middleware?.length) {
    for (const m of options.middleware) raw.use(m);
  }

  const throwOnError = options.throwOnError === true;

  // Wrap an openapi-fetch result into the SDK's SdkResult envelope. We accept
  // any "thenable" with the data/error/response triple so we don't depend on
  // openapi-fetch's exact internal typings (which have shifted several times).
  async function wrap<T>(
    method: string,
    urlPath: string,
    rawResult: unknown,
  ): Promise<SdkResult<T>> {
    const r = (await rawResult) as { data: T | undefined; error: unknown; response: Response };
    const { data, error, response } = r;
    const status = response.status;
    const parsedError: ApiErrorBody | undefined =
      error && typeof error === 'object' ? (error as ApiErrorBody) : undefined;

    if (throwOnError && (status < 200 || status >= 300)) {
      throw new StellarBillError({
        status,
        body: parsedError,
        requestUrl: urlPath,
        requestMethod: method,
        message: makeErrorMessage(method, urlPath, status, parsedError),
      });
    }

    return { data, error: parsedError, status, response, requestMethod: method, requestUrl: response.url || urlPath };
  }

  return {
    raw,
    version: SDK_VERSION,
    setToken(token) {
      tokenHolder.set(sanitizeToken(token));
    },
    getToken() {
      return tokenHolder.get();
    },

    getHealth: async () => {
      const res = await raw.GET('/api/health', {});
      return wrap('GET', '/api/health', res);
    },

    listPlans: async (params: ListPlansParams = {}) => {
      const query: Record<string, string | number> = {};
      if (params.cursor !== undefined) query['cursor'] = params.cursor;
      if (params.limit !== undefined) query['limit'] = params.limit;
      const res = await raw.GET('/api/v1/plans', { params: { query } });
      return wrap('GET', '/api/v1/plans', res);
    },

    listSubscriptions: async (params: ListSubscriptionsParams = {}) => {
      const query: Record<string, string | number> = {};
      if (params.cursor !== undefined) query['cursor'] = params.cursor;
      if (params.limit !== undefined) query['limit'] = params.limit;
      const res = await raw.GET('/api/subscriptions', { params: { query } });
      return wrap('GET', '/api/subscriptions', res);
    },

    getSubscription: async (id: string) => {
      if (typeof id !== 'string' || id.length === 0) {
        throw new StellarBillConfigError('subscription id must be a non-empty string');
      }
      const path = encodeURIComponent(id);
      const res = await raw.GET('/api/subscriptions/{id}', { params: { path: { id } } });
      return wrap('GET', `/api/subscriptions/${path}`, res);
    },

    inspectIdempotencyKey: async (key: string) => {
      if (typeof key !== 'string' || key.length === 0) {
        throw new StellarBillConfigError('idempotency key must be a non-empty string');
      }
      if (key.length > 255) {
        throw new StellarBillConfigError('idempotency key exceeds maximum length of 255 characters');
      }
      const path = encodeURIComponent(key);
      const res = await raw.GET('/api/v1/idempotency/{key}', { params: { path: { key } } });
      return wrap('GET', `/api/v1/idempotency/${path}`, res);
    },
  };
}

/**
 * Manual helper for callers that want throw-on-error semantics on a single
 * call without enabling `throwOnError` globally.
 */
export async function assertOk<T>(result: SdkResult<T>): Promise<T> {
  if (result.status >= 200 && result.status < 300) {
    if (result.data === undefined) {
      throw new StellarBillError({
        status: result.status,
        body: undefined,
        requestUrl: result.requestUrl,
        requestMethod: result.requestMethod,
        message: `${result.requestMethod} ${result.requestUrl} returned 2xx with empty body`,
      });
    }
    return result.data;
  }
  throw new StellarBillError({
    status: result.status,
    body: result.error,
    requestUrl: result.requestUrl,
    requestMethod: result.requestMethod,
    message: makeErrorMessage(result.requestMethod, result.requestUrl, result.status, result.error),
  });
}

/**
 * Re-export for callers that want to construct or inspect API error
 * envelopes manually (e.g. for response-shape validation in their own tests).
 */
export { safeParseErrorBody };
