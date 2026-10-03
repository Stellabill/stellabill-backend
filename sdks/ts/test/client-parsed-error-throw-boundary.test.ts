/**
 * parsedError boundary-condition tests — issue #961
 *
 * The branch named by the issue lives inside `wrap()` in `src/client.ts`:
 *
 *   const parsedError: ApiErrorBody | undefined =
 *     error && typeof error === 'object' ? (error as ApiErrorBody) : undefined;
 *
 *   if (throwOnError && (status < 200 || status >= 300)) {
 *     throw new StellarBillError({
 *       status,
 *       body: parsedError,   // <-- the branch from client.ts:229
 *       ...
 *     });
 *   }
 *
 * `wrap()` is private, so every case is driven through the public
 * `createStellarBillClient()` surface (`getHealth()`), which is what callers
 * actually observe. Two paths are pinned:
 *
 *   • non-throwing path  -> `result.error` is exactly `parsedError`
 *   • throwing path      -> `StellarBillError.body` is exactly `parsedError`
 *
 * Guard boundary map for `error && typeof error === 'object'`:
 *   A. `undefined`   (2xx success / unparseable body)  -> undefined
 *   B. `null`        (falsy short-circuit)             -> undefined
 *   C. string        (typeof !== 'object')             -> undefined
 *   D. number        (typeof !== 'object')             -> undefined
 *   E. boolean false (falsy short-circuit)             -> undefined
 *   F. boolean true  (typeof !== 'object')             -> undefined
 *   G. array         (truthy object, guard passes)     -> the array
 *   H. plain object  (truthy object, normal case)      -> the object
 *
 * The success (`data`/2xx) and failure (thrown error/non-2xx) paths are both
 * asserted so the SDK contract stays stable and deterministic.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

const BASE_URL = 'https://api.example.com';

type MockFetch = typeof globalThis.fetch;

/**
 * Build a fetch mock that always resolves to a fresh Response so each call
 * gets its own readable body. `contentType` defaults to JSON so
 * openapi-fetch parses the body into the `error` field on non-2xx.
 */
function mockFetch(
  body: string | null,
  init: { status: number; contentType?: string } = { status: 200 },
): MockFetch {
  const contentType = init.contentType ?? 'application/json';
  return vi.fn(async () => new Response(body, { status: init.status, headers: { 'content-type': contentType } }));
}

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// A — error is undefined (2xx). Success path must not throw and error stays
// undefined; throwOnError=true must not fire inside the accept band.
// ---------------------------------------------------------------------------
describe('parsedError boundary A — undefined error (2xx success)', () => {
  it('does not throw and leaves result.error undefined on 200 with throwOnError disabled', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch: mockFetch(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), { status: 200 }),
    });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
  });

  it('does not throw on 204 with throwOnError enabled (success path preserved)', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, throwOnError: true, fetch: mockFetch(null, { status: 204 }) });

    const r = await sdk.getHealth();

    expect(r.status).toBe(204);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// B — error is null. The guard's leading `error &&` short-circuits, so
// parsedError must be undefined and the thrown error must carry body undefined.
// ---------------------------------------------------------------------------
describe('parsedError boundary B — null error', () => {
  it('yields result.error === undefined for a JSON `null` body (non-throwing path)', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch: mockFetch('null', { status: 400 }) });

    const r = await sdk.getHealth();

    expect(r.status).toBe(400);
    expect(r.error).toBeUndefined();
  });

  it('throws a StellarBillError with body undefined when throwOnError is set', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, throwOnError: true, fetch: mockFetch('null', { status: 503 }) });

    const err = await sdk.getHealth().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(StellarBillError);
    expect(err).toMatchObject({ status: 503, body: undefined });
  });
});

// ---------------------------------------------------------------------------
// C / D / E / F — error is a truthy-or-falsy primitive. `typeof` is never
// 'object', so the guard yields undefined regardless of truthiness.
// ---------------------------------------------------------------------------
describe('parsedError boundary C–F — primitive error values', () => {
  it.each([
    ['string', '"Forbidden"'],
    ['number', '404'],
    ['boolean false', 'false'],
    ['boolean true', 'true'],
  ])('treats a JSON %s body as parsedError === undefined (non-throwing path)', async (_label, body) => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch: mockFetch(body, { status: 400 }) });

    const r = await sdk.getHealth();

    expect(r.status).toBe(400);
    expect(r.error).toBeUndefined();
  });

  it.each([
    ['string', '"Unauthorized"', 401],
    ['number', '422', 422],
    ['boolean true', 'true', 400],
  ])('passes body=undefined to StellarBillError for a primitive %s body', async (_label, body, status) => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, throwOnError: true, fetch: mockFetch(body, { status }) });

    const err = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;

    expect(err).toBeInstanceOf(StellarBillError);
    expect(err.status).toBe(status);
    expect(err.body).toBeUndefined();
    // makeErrorMessage falls back to `HTTP ${status}` when there is no body detail.
    expect(err.message).toContain(`HTTP ${status}`);
  });
});

// ---------------------------------------------------------------------------
// G — error is an array. Arrays are truthy objects, so the guard passes and
// the SDK surfaces the array as-is. This test pins that observable behaviour
// (the SDK deliberately does not call normalizeErrorBody inside wrap()).
// ---------------------------------------------------------------------------
describe('parsedError boundary G — array error passes the object guard', () => {
  it('exposes the array on result.error without throwing (non-throwing path)', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch: mockFetch('[{"code":"e1"},{"code":"e2"}]', { status: 422 }),
    });

    const r = await sdk.getHealth();

    expect(r.status).toBe(422);
    expect(Array.isArray(r.error)).toBe(true);
    expect(r.error).toEqual([{ code: 'e1' }, { code: 'e2' }]);
  });

  it('passes the same array through to StellarBillError.body when throwOnError is set', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      throwOnError: true,
      fetch: mockFetch('[{"code":"e1"}]', { status: 422 }),
    });

    const err = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;

    expect(err).toBeInstanceOf(StellarBillError);
    expect(err.status).toBe(422);
    expect(err.body).toEqual([{ code: 'e1' }]);
  });
});

// ---------------------------------------------------------------------------
// H — error is a plain object: the normal structured error case. parsedError
// must be the object itself, on both the result envelope and the thrown error.
// ---------------------------------------------------------------------------
describe('parsedError boundary H — plain object error', () => {
  const body = { error: 'Bad Request', message: 'invalid cursor', code: 'invalid_cursor' };

  it('returns the parsed body on result.error (non-throwing path)', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch: mockFetch(JSON.stringify(body), { status: 400 }) });

    const r = await sdk.getHealth();

    expect(r.status).toBe(400);
    expect(r.error).toEqual(body);
    expect(r.data).toBeUndefined();
  });

  it('uses parsedError as StellarBillError.body and derives the message from it', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      throwOnError: true,
      fetch: mockFetch(JSON.stringify(body), { status: 400 }),
    });

    const err = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;

    expect(err).toBeInstanceOf(StellarBillError);
    expect(err.status).toBe(400);
    expect(err.body).toEqual(body);
    expect(err.message).toBe('GET /api/health failed (400): invalid cursor');
  });
});

// ---------------------------------------------------------------------------
// Cross-cutting — body shape must not be parsed on the wire when the branch
// is exercised through a different SDK method, and non-JSON responses must
// yield undefined rather than an ambiguous value.
// ---------------------------------------------------------------------------
describe('parsedError boundary — method and content-type independence', () => {
  it('applies the same parsedError contract through getSubscription', async () => {
    const body = { message: 'Resource not found' };
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      throwOnError: true,
      fetch: mockFetch(JSON.stringify(body), { status: 404 }),
    });

    const err = (await sdk.getSubscription('sub_missing').catch((e: unknown) => e)) as StellarBillError;

    expect(err.status).toBe(404);
    expect(err.body).toEqual(body);
    expect(err.requestUrl).toBe('/api/subscriptions/sub_missing');
  });

  it('yields parsedError === undefined for a non-JSON error body (no ambiguous value)', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      throwOnError: true,
      fetch: mockFetch('<html>Server Error</html>', { status: 502, contentType: 'text/html' }),
    });

    const err = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;

    expect(err.status).toBe(502);
    expect(err.body).toBeUndefined();
    expect(err.message).toContain('HTTP 502');
  });

  it('yields parsedError === undefined for an empty JSON error body', async () => {
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch: mockFetch('', { status: 503 }),
    });

    const r = await sdk.getHealth();

    expect(r.status).toBe(503);
    expect(r.error).toBeUndefined();
  });
});
