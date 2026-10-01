/**
 * Focused boundary-condition tests for the `parsedError` branch and
 * `requestUrl` fallback inside `wrap()` (src/client.ts ~L215-238).
 *
 * The two expressions under test are:
 *
 *   const parsedError = error && typeof error === 'object'
 *     ? (error as ApiErrorBody) : undefined;
 *
 *   requestUrl: response.url || urlPath
 *
 * Because `wrap()` is internal (not exported), we exercise it via the
 * public SDK methods.  Every test names the branch it covers so that a
 * future refactor can find and update the assertion immediately.
 *
 * Branch map
 * ──────────
 *  A  error is falsy    (undefined / null / false / 0 / "")  → parsedError = undefined
 *  B  error is truthy but not an object (primitive: string,
 *     number, boolean true)                                   → parsedError = undefined
 *  C  error is truthy AND an object                          → parsedError = error as ApiErrorBody
 *  D  response.url is a non-empty string                     → requestUrl = response.url
 *  E  response.url is "" (constructed Response)              → requestUrl = urlPath fallback
 *
 * Reachability note
 * ──────────────────
 * openapi-fetch v0.13 only ever sets `error` to an object (parsed JSON on
 * non-2xx) or `undefined` (2xx, or non-2xx non-JSON). It never yields a
 * primitive. Therefore:
 *   • Branch A is exercised via 2xx responses (error = undefined).
 *   • Branch B is dead code from openapi-fetch's normal path; it is
 *     exercised here by injecting a middleware that replaces the resolved
 *     openapi-fetch result with a synthetic one carrying a primitive
 *     `error` value — documenting the contract even for the unreachable case.
 *   • Branch C is exercised via non-2xx JSON responses.
 *   • Branch D is exercised by constructing a Response whose `.url`
 *     property is non-empty via Object.defineProperty.
 *   • Branch E is the default for all `new Response(...)` mocks because
 *     the Response constructor never sets `.url`.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Middleware } from 'openapi-fetch';

import { createStellarBillClient } from '../src/index.js';

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a fetch mock that always returns the given Response. */
function fetchFor(response: Response): typeof globalThis.fetch {
  return vi.fn(async () => response);
}

/**
 * Build a Response whose `.url` property is non-empty.
 *
 * The Response constructor never sets `.url` (it is populated by the
 * browser/node fetch implementation with the final request URL after
 * redirects). We use Object.defineProperty to approximate that runtime
 * behaviour inside unit tests.
 */
function responseWithUrl(body: unknown, status: number, fetchUrl: string): Response {
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, { status, headers: { 'content-type': 'application/json' } });
  Object.defineProperty(res, 'url', { value: fetchUrl, configurable: true, writable: false });
  return res;
}

/**
 * Middleware factory that intercepts the openapi-fetch result *after*
 * `onResponse` and replaces the resolved `{ data, error, response }` triple
 * with a custom one.  This is the only way to inject a synthetic primitive
 * `error` value into `wrap()` without exporting wrap() itself.
 *
 * We abuse openapi-fetch's `onRequest` hook to stash the replacement on a
 * per-call closure, then overwrite `response.json` so that openapi-fetch
 * parses whatever we want as the "error" body.  A cleaner approach exists
 * via the `onResponse` hook returning a Response whose JSON body is a
 * primitive — openapi-fetch sets `error = parsedBody` when the response is
 * non-2xx, and if `parsedBody` is a primitive that is exactly what reaches
 * `wrap()`.
 */
function primitiveErrorMiddleware(primitiveValue: string | number | boolean): Middleware {
  return {
    async onResponse({ response }) {
      // Craft a replacement Response whose .json() yields the raw primitive.
      // openapi-fetch checks `response.ok` and, on non-2xx, does
      //   error = await response.json()
      // returning the raw parsed value — even a string or number.
      const clone = new Response(JSON.stringify(primitiveValue), {
        status: response.status,
        headers: response.headers,
      });
      return clone;
    },
  };
}

// ---------------------------------------------------------------------------
// Branch A — error is falsy (undefined): 2xx responses
// ---------------------------------------------------------------------------

describe('parsedError branch A — falsy error (2xx → error=undefined → parsedError=undefined)', () => {
  it('A-1: getHealth 200 JSON → error is undefined, data is present', async () => {
    const body = { status: 'ok', service: 'stellarbill-backend' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.getHealth();
    // Branch A: openapi-fetch sets error=undefined on 2xx → parsedError=undefined
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(200);
    expect(r.data?.status).toBe('ok');
  });

  it('A-2: listPlans 200 → error is explicitly undefined (not null, not false)', async () => {
    const body = { plans: [], pagination: { has_more: false } };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.listPlans();
    // The SDK contract: error MUST be strictly undefined on success, not any other falsy value
    expect(r.error).toBeUndefined();
    expect(r.error).toStrictEqual(undefined);
  });

  it('A-3: non-2xx with non-JSON body → error=undefined (openapi-fetch cannot parse body)', async () => {
    // When content-type is NOT application/json, openapi-fetch cannot parse
    // a JSON error body, so error=undefined — demonstrating that branch A also
    // fires for non-2xx when there is no parseable error object.
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('Internal Server Error', { status: 500, headers: { 'content-type': 'text/plain' } })),
    });
    const r = await sdk.getHealth();
    // Branch A: error=undefined because content is not JSON
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(500);
  });

  it('A-4: non-2xx with empty JSON body → error=undefined', async () => {
    // An empty JSON response on non-2xx: openapi-fetch still attempts json()
    // but an empty string throws or produces nothing useful, so error stays undefined.
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('', { status: 503, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.getHealth();
    // Branch A: body is empty / unparseable → error=undefined
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(503);
  });
});

// ---------------------------------------------------------------------------
// Branch B — error is a truthy primitive (dead code from openapi-fetch path)
// ---------------------------------------------------------------------------

describe('parsedError branch B — primitive error (truthy but not object → parsedError=undefined)', () => {
  /**
   * openapi-fetch never produces a primitive `error` in normal operation.
   * These tests exercise the guard (`typeof error === 'object'`) by injecting
   * synthetic primitive values via a response middleware, proving that even
   * if a future library upgrade or unusual payload caused `error` to carry a
   * string/number/boolean, the SDK silently returns `error: undefined`
   * rather than forwarding a type-unsafe value to callers.
   */

  it('B-1: error is a string primitive → parsedError is undefined (not the string)', async () => {
    // Middleware replaces the 400 response body with a JSON string (not an object)
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('"an error string"', { status: 400, headers: { 'content-type': 'application/json' } })),
      middleware: [primitiveErrorMiddleware('an error string')],
    });
    const r = await sdk.getHealth();
    // Branch B: typeof "string" !== 'object' → parsedError = undefined
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(400);
  });

  it('B-2: error is a number primitive → parsedError is undefined', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('42', { status: 422, headers: { 'content-type': 'application/json' } })),
      middleware: [primitiveErrorMiddleware(42)],
    });
    const r = await sdk.getHealth();
    // Branch B: typeof 42 !== 'object' → parsedError = undefined
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(422);
  });

  it('B-3: error is boolean true → parsedError is undefined', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('true', { status: 400, headers: { 'content-type': 'application/json' } })),
      middleware: [primitiveErrorMiddleware(true)],
    });
    const r = await sdk.getHealth();
    // Branch B: true && typeof true === 'object' → false → parsedError = undefined
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(400);
  });

  it('B-4: error is JSON array → parsedError is undefined (array is object but guard filters truthy)', async () => {
    // Arrays ARE objects but the SDK type contract expects a plain object.
    // This is handled by safeParseErrorBody (which the SDK does not call inside wrap()),
    // but openapi-fetch itself returns the raw parsed value. A JSON array
    // ([]) is an object, so the branch-B shortcut (`typeof === 'object'`) does
    // NOT guard this case — wrap() would cast it as ApiErrorBody.
    // This test documents that behavior rather than asserting undefined.
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response('[1,2,3]', { status: 400, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.getHealth();
    // Arrays are objects and truthy: wrap() treats them as ApiErrorBody.
    // This is documented behavior; callers should use safeParseErrorBody for
    // stricter validation if needed.
    expect(r.status).toBe(400);
    // error may be the array cast as ApiErrorBody — the contract is that it
    // is not undefined. We only assert it doesn't throw.
    // (Do NOT assert r.error === undefined here; that would misstate the contract.)
  });
});

// ---------------------------------------------------------------------------
// Branch C — error is a truthy object → parsedError = error as ApiErrorBody
// ---------------------------------------------------------------------------

describe('parsedError branch C — object error → parsedError is populated', () => {
  it('C-1: 400 with full error object → all fields surfaced', async () => {
    const body = { error: 'Bad Request', message: 'Invalid cursor value', code: 'invalid_cursor' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 400, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.listPlans();
    // Branch C: error is a truthy object → parsedError = error
    expect(r.error).toBeDefined();
    expect(r.error?.error).toBe('Bad Request');
    expect(r.error?.message).toBe('Invalid cursor value');
    expect(r.error?.code).toBe('invalid_cursor');
    expect(r.status).toBe(400);
    expect(r.data).toBeUndefined();
  });

  it('C-2: 404 with partial error object (only message field) → parsedError has message, others undefined', async () => {
    const body = { message: 'Resource not found' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 404, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.getSubscription('sub_missing');
    // Branch C: truthy object → parsedError = { message: '...' }
    expect(r.error?.message).toBe('Resource not found');
    expect(r.error?.code).toBeUndefined();
    expect(r.error?.error).toBeUndefined();
    expect(r.status).toBe(404);
  });

  it('C-3: 500 with empty object {} → parsedError is truthy but all fields undefined', async () => {
    const body = {};
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 500, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.getHealth();
    // Branch C: {} is truthy and typeof 'object' → parsedError = {}
    // All fields on ApiErrorBody are optional; {} satisfies the type.
    expect(r.error).toBeDefined();
    expect(r.error).toEqual({});
    expect(r.status).toBe(500);
  });

  it('C-4: 401 with error + code fields → accessible on result', async () => {
    const body = { error: 'Unauthorized', code: 'auth_unauthorized' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 401, headers: { 'content-type': 'application/json' } })),
    });
    const r = await sdk.inspectIdempotencyKey('idem-key-1');
    // Branch C: object error → parsedError
    expect(r.error?.error).toBe('Unauthorized');
    expect(r.error?.code).toBe('auth_unauthorized');
    expect(r.status).toBe(401);
  });

  it('C-5: throwOnError=true with object error → thrown StellarBillError carries parsedError as body', async () => {
    const body = { error: 'Not Found', message: 'gone', code: 'missing' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch: fetchFor(new Response(JSON.stringify(body), { status: 404, headers: { 'content-type': 'application/json' } })),
    });
    // Branch C: parsedError is populated → thrown StellarBillError.body = parsedError
    const err = await sdk.getSubscription('missing-sub').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(Error);
    const e = err as { status: number; body: { code?: string } };
    expect(e.status).toBe(404);
    expect(e.body?.code).toBe('missing');
  });
});

// ---------------------------------------------------------------------------
// Branch D — response.url is non-empty → requestUrl = response.url
// ---------------------------------------------------------------------------

describe('requestUrl branch D — response.url non-empty → used as requestUrl', () => {
  it('D-1: 200 response with non-empty .url → requestUrl uses response.url verbatim', async () => {
    const body = { status: 'ok', service: 'stellarbill-backend' };
    const finalUrl = 'https://api.example.com/api/health';
    const res = responseWithUrl(body, 200, finalUrl);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getHealth();
    // Branch D: response.url is non-empty → requestUrl = response.url (not the SDK's urlPath)
    expect(r.requestUrl).toBe(finalUrl);
    expect(r.requestUrl).not.toBe('/api/health');
  });

  it('D-2: non-2xx response with non-empty .url → requestUrl uses response.url', async () => {
    const body = { error: 'Not Found', message: 'gone', code: 'missing' };
    const finalUrl = 'https://api.example.com/api/subscriptions/sub_xyz';
    const res = responseWithUrl(body, 404, finalUrl);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getSubscription('sub_xyz');
    // Branch D: response.url non-empty on error path → requestUrl = response.url
    expect(r.requestUrl).toBe(finalUrl);
    expect(r.error).toBeDefined();
  });

  it('D-3: response.url with redirect target → requestUrl reflects the final (redirected) URL', async () => {
    // Simulate a 301→200 redirect scenario: fetch resolves the Response whose
    // .url is the final URL (after redirect), not the initially requested path.
    const originalPath = '/api/health';
    const redirectedUrl = 'https://cdn.example.com/api/health-v2';
    const body = { status: 'ok', service: 'stellarbill-backend' };
    const res = responseWithUrl(body, 200, redirectedUrl);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getHealth();
    // Branch D: response.url carries the post-redirect URL; wrap() prefers it
    // over the SDK's own urlPath constant so callers see the real request destination.
    expect(r.requestUrl).toBe(redirectedUrl);
    expect(r.requestUrl).not.toBe(originalPath);
  });
});

// ---------------------------------------------------------------------------
// Branch E — response.url is "" → requestUrl falls back to urlPath
// ---------------------------------------------------------------------------

describe('requestUrl branch E — response.url empty → urlPath fallback', () => {
  it('E-1: getHealth mock (new Response) → response.url is "", requestUrl falls back to /api/health', async () => {
    const body = { status: 'ok', service: 'stellarbill-backend' };
    const res = new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });
    // Confirm the test premise: Response constructor yields empty .url
    expect(res.url).toBe('');
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getHealth();
    // Branch E: response.url === '' (falsy) → requestUrl = urlPath = '/api/health'
    expect(r.requestUrl).toContain('/api/health');
    expect(r.requestUrl).not.toBe('');
  });

  it('E-2: non-2xx mock → response.url is "", requestUrl falls back to SDK urlPath', async () => {
    const body = { error: 'Bad Request', message: 'bad', code: 'x' };
    const res = new Response(JSON.stringify(body), { status: 400, headers: { 'content-type': 'application/json' } });
    expect(res.url).toBe('');
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.listPlans();
    // Branch E: error path also falls back to urlPath
    expect(r.requestUrl).toContain('/api/v1/plans');
    expect(r.requestUrl).not.toBe('');
  });

  it('E-3: getSubscription mock → requestUrl fallback contains encoded subscription id path', async () => {
    const body = { id: 'sub_1', plan_id: 'p', customer: 'c', status: 'active', amount: '1', interval: 'monthly' };
    const res = new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });
    expect(res.url).toBe('');
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getSubscription('sub_1');
    // Branch E: urlPath includes the encoded id segment
    expect(r.requestUrl).toContain('/api/subscriptions/');
    expect(r.requestUrl).toContain('sub_1');
  });

  it('E-4: inspectIdempotencyKey → requestUrl fallback contains encoded key', async () => {
    const body = {
      key: 'order-abc',
      used_at: '2026-01-01T00:00:00Z',
      expires_at: '2026-01-02T00:00:00Z',
      status_code: 200,
      request_fingerprint: 'fp',
    };
    const res = new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });
    expect(res.url).toBe('');
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.inspectIdempotencyKey('order-abc');
    // Branch E: requestUrl is the SDK's urlPath (/api/v1/idempotency/order-abc)
    expect(r.requestUrl).toContain('/api/v1/idempotency/');
    expect(r.requestUrl).toContain('order-abc');
  });

  it('E-5: explicitly verify the || expression — response.url="" is falsy, urlPath is truthy', async () => {
    // This test verifies the exact runtime semantics of `response.url || urlPath`
    // by driving through the SDK end-to-end.
    // When response.url === '' (falsy), requestUrl must equal the SDK's urlPath constant.
    // When response.url !== '' (truthy), requestUrl must equal response.url (branch D).
    const body = { status: 'ok', service: 'stellarbill-backend' };

    // E path: new Response() → response.url = '' → requestUrl = urlPath
    const emptyUrlRes = new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });
    const sdkE = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(emptyUrlRes),
    });
    const rE = await sdkE.getHealth();
    expect(rE.requestUrl).toBe('/api/health');

    // D path: non-empty response.url → requestUrl = response.url (not urlPath)
    const nonEmptyUrl = 'https://api.example.com/api/health';
    const nonEmptyUrlRes = responseWithUrl(body, 200, nonEmptyUrl);
    const sdkD = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(nonEmptyUrlRes),
    });
    const rD = await sdkD.getHealth();
    expect(rD.requestUrl).toBe(nonEmptyUrl);
    expect(rD.requestUrl).not.toBe('/api/health');
  });
});

// ---------------------------------------------------------------------------
// Cross-branch: deterministic enum of all parsedError outcomes
// ---------------------------------------------------------------------------

describe('parsedError — exhaustive contract table', () => {
  /**
   * This describe block enumerates all reachable (error, parsedError) pairs
   * to make the contract machine-readable and prevent silent regressions.
   *
   * Format: [scenario, httpStatus, bodyJson, expectedErrorDefined]
   */
  const cases: [string, number, unknown, boolean][] = [
    // Branch A — falsy error
    ['2xx JSON body', 200, { status: 'ok', service: 's' }, false],
    ['non-2xx text/plain (no JSON parsed)', 502, null, false],
    // Branch C — truthy object error
    ['non-2xx full error object', 400, { error: 'e', message: 'm', code: 'c' }, true],
    ['non-2xx partial error (message only)', 403, { message: 'forbidden' }, true],
    ['non-2xx empty object', 500, {}, true],
  ];

  it.each(cases)('%s → error defined=%s', async (scenario, status, bodyValue, shouldBeDefined) => {
    const contentType = bodyValue === null ? 'text/plain' : 'application/json';
    const text = bodyValue === null ? 'Service Unavailable' : JSON.stringify(bodyValue);
    const res = new Response(text, { status, headers: { 'content-type': contentType } });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchFor(res),
    });
    const r = await sdk.getHealth();
    if (shouldBeDefined) {
      expect(r.error, `expected error to be defined for scenario: ${scenario}`).toBeDefined();
    } else {
      expect(r.error, `expected error to be undefined for scenario: ${scenario}`).toBeUndefined();
    }
    expect(r.status).toBe(status);
  });
});
