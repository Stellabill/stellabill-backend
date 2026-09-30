/**
 * parsedError boundary condition tests — issue #970
 *
 * Covers every observable branch of the `parsedError` expression inside
 * `wrap()` in `src/client.ts`:
 *
 *   const parsedError: ApiErrorBody | undefined =
 *     error && typeof error === 'object' ? (error as ApiErrorBody) : undefined;
 *
 * Branches:
 *   A. error is undefined          → parsedError === undefined  (success 2xx)
 *   B. error is null               → falsy, parsedError === undefined
 *   C. error is a string           → non-object, parsedError === undefined
 *   D. error is a number           → non-object, parsedError === undefined
 *   E. error is a boolean (false)  → falsy,     parsedError === undefined
 *   F. error is an array           → typeof [] === 'object' + truthy → passes guard
 *                                    parsedError = [] as ApiErrorBody (non-null object)
 *   G. error is a plain object     → parsedError = the object (normal error path)
 *
 * Also covers the `getToken()` boundary at client.ts:248 (`return tokenHolder.get()`):
 *   H. getToken() returns undefined when no token has ever been set
 *   I. getToken() returns undefined after setToken(undefined)
 *   J. getToken() returns the current token without mutating it
 *
 * Each test drives behavior through the public SDK surface so the assertions
 * are observable by callers and are not implementation-detail-only.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

// ---------------------------------------------------------------------------
// Shared mock helpers (same pattern as client.test.ts to stay consistent)
// ---------------------------------------------------------------------------

/**
 * Returns a fetch mock that delivers a canned Response.  The `error` field
 * openapi-fetch exposes on its result is driven entirely by the HTTP body +
 * status code.  We use a raw Response so we can control what openapi-fetch
 * parses as the error value.
 */
function mockResponse(
  body: string,
  init: { status: number; contentType?: string },
): { fetch: typeof globalThis.fetch } {
  const res = new Response(body, {
    status: init.status,
    headers: { 'content-type': init.contentType ?? 'application/json' },
  });
  const fetch: typeof globalThis.fetch = vi.fn(async () => res);
  return { fetch };
}

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Branch A – error is undefined (2xx success, no error from openapi-fetch)
// parsedError must remain undefined and not appear in the result envelope.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch A: error is undefined (2xx success)', () => {
  it('result.error is undefined when the response is 200 with a valid body', async () => {
    const { fetch } = mockResponse(
      JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }),
      { status: 200 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeDefined();
  });
});

// ---------------------------------------------------------------------------
// Branch B – error is null
// openapi-fetch sets error = parsed JSON body on non-2xx.  A body of `null`
// produces a null value.  The guard `error && …` is falsy for null, so
// parsedError must be undefined.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch B: error field is null', () => {
  it('result.error is undefined when the response body parses to null', async () => {
    // "null" is valid JSON but typeof null === "object" — however the guard
    // starts with `error && …` so null short-circuits to false → undefined.
    const { fetch } = mockResponse('null', { status: 400 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(400);
    // parsedError must be undefined, not the null value itself.
    expect(r.error).toBeUndefined();
  });

  it('throwOnError + null body uses undefined as StellarBillError.body', async () => {
    const { fetch } = mockResponse('null', { status: 503 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const thrown = await sdk.getHealth().catch((e: unknown) => e);
    expect(thrown).toMatchObject({ status: 503, body: undefined });
  });
});

// ---------------------------------------------------------------------------
// Branch C – error is a string primitive
// A non-2xx response with a body that is a bare JSON string (e.g. `"Forbidden"`)
// produces a string `error` field in openapi-fetch.  The guard
// `typeof error === 'object'` fails → parsedError must be undefined.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch C: error field is a string primitive', () => {
  it('result.error is undefined when the error body is a JSON string', async () => {
    const { fetch } = mockResponse('"Forbidden"', { status: 403 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(403);
    // A string passes `typeof` check as 'string' not 'object', so parsedError → undefined.
    expect(r.error).toBeUndefined();
  });

  it('throwOnError + string body stores undefined on StellarBillError.body', async () => {
    const { fetch } = mockResponse('"Unauthorized"', { status: 401 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const thrown = await sdk.getHealth().catch((e: unknown) => e);
    expect(thrown).toMatchObject({ status: 401, body: undefined });
  });
});

// ---------------------------------------------------------------------------
// Branch D – error is a number primitive
// A bare JSON number body (e.g. `404`) produces a numeric `error` field.
// `typeof 404 === 'number'` → fails the object guard → parsedError = undefined.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch D: error field is a number primitive', () => {
  it('result.error is undefined when the error body is a JSON number', async () => {
    const { fetch } = mockResponse('404', { status: 404 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(404);
    expect(r.error).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Branch E – error is a boolean false (falsy non-object)
// `false && …` short-circuits to false → parsedError = undefined.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch E: error field is boolean false', () => {
  it('result.error is undefined when the error body is JSON false', async () => {
    const { fetch } = mockResponse('false', { status: 400 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(400);
    expect(r.error).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Branch F – error is an array (non-null object, truthy)
// `typeof [] === 'object'` is true and arrays are truthy, so the guard
// PASSES and parsedError is assigned the array cast as ApiErrorBody.
// This means result.error will be the array itself (not undefined).
// This test pins that observable behavior so any future change is caught.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch F: error field is an array', () => {
  it('result.error is the array value (array passes the object guard)', async () => {
    // A JSON array body puts an array into the `error` field from openapi-fetch.
    const { fetch } = mockResponse('[{"message":"err1"},{"message":"err2"}]', { status: 422 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(422);
    // Arrays are typeof 'object' and truthy, so the guard passes.
    // The contract: result.error reflects whatever the guard yields.
    expect(Array.isArray(r.error)).toBe(true);
  });

  it('throwOnError + array body stores the array on StellarBillError.body', async () => {
    const { fetch } = mockResponse('[{"code":"e1"}]', { status: 422 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const thrown = await sdk.getHealth().catch((e: unknown) => e);
    expect(thrown).toMatchObject({ status: 422 });
    // body reflects the raw array — this pins the current contract.
    expect(Array.isArray((thrown as { body: unknown }).body)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Branch G – error is a plain object (the normal structured error case)
// This is the primary success path for non-2xx errors; tests here exist in
// client.test.ts but we include a focused assertion to close the branch matrix.
// ---------------------------------------------------------------------------
describe('parsedError boundary – Branch G: error field is a plain object', () => {
  it('result.error is the parsed ApiErrorBody when the body is a plain object', async () => {
    const { fetch } = mockResponse(
      JSON.stringify({ error: 'Bad Request', message: 'invalid param', code: 'invalid_param' }),
      { status: 400 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(400);
    expect(r.error).toMatchObject({ error: 'Bad Request', code: 'invalid_param' });
  });

  it('parsedError object is passed through to StellarBillError.body when throwOnError', async () => {
    const { fetch } = mockResponse(
      JSON.stringify({ error: 'Not Found', message: 'gone', code: 'not_found' }),
      { status: 404 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const thrown = await sdk.getHealth().catch((e: unknown) => e);
    expect(thrown).toMatchObject({ status: 404, body: { code: 'not_found' } });
  });

  it('non-JSON content-type produces undefined parsedError regardless of status', async () => {
    const { fetch } = mockResponse('<html>Server Error</html>', {
      status: 502,
      contentType: 'text/html',
    });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(502);
    expect(r.error).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Branch H / I / J – getToken() at client.ts:248 (`return tokenHolder.get()`)
//
// The `getToken()` method's single return statement has three observable
// boundary conditions: returning undefined when never set (H), returning
// undefined after explicit clear (I), and returning the current token
// value without side effects (J).
// ---------------------------------------------------------------------------
describe('getToken() boundary – client.ts:248 (return tokenHolder.get())', () => {
  it('H: returns undefined when no token is provided at construction', () => {
    const { fetch } = mockResponse('{}', { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    // No token option → TokenHolder is initialized with undefined.
    expect(sdk.getToken()).toBeUndefined();
  });

  it('I: returns undefined after setToken(undefined) clears the token', () => {
    const { fetch } = mockResponse('{}', { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'initial', fetch });
    expect(sdk.getToken()).toBe('initial');
    sdk.setToken(undefined);
    // After clearing, getToken() must return undefined, not the old value.
    expect(sdk.getToken()).toBeUndefined();
  });

  it('I: returns undefined after setToken with whitespace-only string (sanitized to undefined)', () => {
    const { fetch } = mockResponse('{}', { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'valid-token', fetch });
    sdk.setToken('   ');
    // sanitizeToken('   ') → undefined, so getToken() must return undefined.
    expect(sdk.getToken()).toBeUndefined();
  });

  it('J: returns the current token without mutating it', () => {
    const { fetch } = mockResponse('{}', { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'my-token', fetch });
    // Calling getToken() multiple times must return the same value each time.
    expect(sdk.getToken()).toBe('my-token');
    expect(sdk.getToken()).toBe('my-token');
    expect(sdk.getToken()).toBe('my-token');
  });

  it('J: getToken() reflects the most recent setToken() call', () => {
    const { fetch } = mockResponse('{}', { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    sdk.setToken('first');
    expect(sdk.getToken()).toBe('first');
    sdk.setToken('second');
    expect(sdk.getToken()).toBe('second');
  });

  it('J: token in getToken() matches the token injected as Authorization header', async () => {
    const calls: string[] = [];
    const res = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    const spyFetch: typeof globalThis.fetch = vi.fn(async (input) => {
      if (input instanceof Request) {
        const auth = input.headers.get('authorization') ?? '';
        calls.push(auth);
      }
      return res;
    });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'live-token', fetch: spyFetch });
    // getToken() and the Authorization header must agree.
    const currentToken = sdk.getToken();
    await sdk.getHealth();
    expect(currentToken).toBe('live-token');
    expect(calls[0]).toBe('Bearer live-token');
  });
});

// ---------------------------------------------------------------------------
// Cross-cutting: requestUrl fallback branch in wrap()
// The return statement `response.url || urlPath` has two branches:
//   • response.url is a non-empty string → use it
//   • response.url is empty string → fall back to urlPath
// ---------------------------------------------------------------------------
describe('parsedError boundary – requestUrl fallback (response.url || urlPath)', () => {
  it('requestUrl falls back to the template path when response.url is empty', async () => {
    // Node's built-in Response has an empty .url when constructed directly.
    const res = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    // Verify our assumption: a directly constructed Response has an empty url.
    expect(res.url).toBe('');
    const fetch: typeof globalThis.fetch = vi.fn(async () => res);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    // When response.url is '' the || operator falls back to the urlPath arg.
    expect(r.requestUrl).toBe('/api/health');
  });

  it('requestUrl uses response.url when the fetch implementation populates it', async () => {
    // Simulate a fetch that returns a Response with a url property set
    // (e.g. after a redirect). We use a Proxy to override the read-only url.
    const base = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    const proxied = new Proxy(base, {
      get(target, prop) {
        if (prop === 'url') return 'https://api.example.com/api/health';
        const val = Reflect.get(target, prop, target);
        return typeof val === 'function' ? val.bind(target) : val;
      },
    });
    const fetch: typeof globalThis.fetch = vi.fn(async () => proxied as Response);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.requestUrl).toBe('https://api.example.com/api/health');
  });
});
