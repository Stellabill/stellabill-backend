import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, safeParseErrorBody, StellarBillError } from '../src/index.js';

/**
 * Boundary tests for the branch at `src/client.ts:116`:
 *
 *   if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
 *     return parsed as ApiErrorBody;
 *   }
 *
 * Three independent sub-conditions decide whether a parsed JSON body is
 * surfaced as an `ApiErrorBody` or collapsed to `undefined`:
 *
 *   1. `parsed` is truthy        -> excludes `null`, `0`, `false`, `""`
 *   2. `typeof parsed === 'object'` -> excludes every JSON primitive
 *   3. `!Array.isArray(parsed)`  -> excludes JSON arrays
 *
 * Each one is pinned below so the SDK's observable error contract cannot
 * silently drift. A new file is used deliberately so this coverage does not
 * overlap with the `safeParseErrorBody` block already in `client.test.ts`.
 */

/** Build a response shaped the way `safeParseErrorBody` expects to encounter. */
function jsonResponse(
  rawBody: string,
  init: { status?: number; contentType?: string } = {},
): Response {
  const status = init.status ?? 400;
  const contentType = init.contentType ?? 'application/json';
  return new Response(rawBody, { status, headers: { 'content-type': contentType } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

// ----- Success path: a JSON object is returned as ApiErrorBody -----

describe('safeParseErrorBody - object branch returns an ApiErrorBody', () => {
  it('returns a well-formed error envelope', async () => {
    const res = jsonResponse(JSON.stringify({ error: 'bad_request', message: 'nope', code: 'E_BAD' }));
    const parsed = await safeParseErrorBody(res);
    expect(parsed).toEqual({ error: 'bad_request', message: 'nope', code: 'E_BAD' });
    expect(parsed?.code).toBe('E_BAD');
  });

  it('returns the empty object {} instead of undefined', async () => {
    // The ambiguous-looking boundary: `{}` is truthy and not an array, so it is
    // surfaced as an *empty* ApiErrorBody rather than `undefined`. Callers must
    // not read this as "no error body was parsed".
    const parsed = await safeParseErrorBody(jsonResponse('{}'));
    expect(parsed).toBeDefined();
    expect(parsed).toEqual({});
    expect(Object.keys(parsed ?? {})).toEqual([]);
    // ...which is why downstream message building falls back to `HTTP <status>`.
    expect(parsed?.message).toBeUndefined();
    expect(parsed?.error).toBeUndefined();
  });

  it('preserves unknown and nested fields verbatim (no schema stripping)', async () => {
    const body = { message: 'boom', traceId: 't-1', details: { field: 'cursor', attempts: [1, 2] } };
    const parsed = await safeParseErrorBody(jsonResponse(JSON.stringify(body)));
    expect(parsed).toEqual(body);
  });

  it('keeps explicit null values for optional fields as-is', async () => {
    const parsed = await safeParseErrorBody(jsonResponse('{"message":null,"code":null}'));
    expect(parsed).toEqual({ message: null, code: null });
  });

  it('returns a plain object, so downstream property lookups are safe', async () => {
    const parsed = await safeParseErrorBody(jsonResponse('{"message":"x"}'));
    expect(Object.getPrototypeOf(parsed)).toBe(Object.prototype);
    expect(Array.isArray(parsed)).toBe(false);
  });

  it('accepts a content-type carrying parameters', async () => {
    const res = jsonResponse('{"message":"nope"}', { contentType: 'application/json; charset=utf-8' });
    expect(await safeParseErrorBody(res)).toEqual({ message: 'nope' });
  });

  it('returns the parsed body for every non-2xx status the API can return', async () => {
    const statuses = [400, 401, 402, 403, 404, 409, 410, 422, 429, 500, 502, 503, 504];
    for (const status of statuses) {
      const res = jsonResponse('{"message":"m","code":"E"}', { status });
      expect(await safeParseErrorBody(res)).toEqual({ message: 'm', code: 'E' });
    }
  });
});

// ----- Failure path: non-object JSON collapses to undefined -----

describe('safeParseErrorBody - non-object JSON collapses to undefined', () => {
  const cases: Array<[label: string, raw: string]> = [
    ['number', '42'],
    ['negative number', '-7'],
    ['float', '1.5'],
    ['zero (falsy primitive)', '0'],
    ['boolean true', 'true'],
    ['boolean false (falsy primitive)', 'false'],
    ['string', '"boom"'],
    ['whitespace string', '"  "'],
    ['empty string (falsy primitive)', '""'],
    ['null', 'null'],
    ['empty array', '[]'],
    ['array of numbers', '[1,2,3]'],
    ['array of error-shaped objects', '[{"message":"m"}]'],
    ['nested array', '[[{"message":"m"}]]'],
  ];

  for (const [label, raw] of cases) {
    it(`returns undefined for a JSON ${label} body`, async () => {
      expect(await safeParseErrorBody(jsonResponse(raw))).toBeUndefined();
    });
  }
});

// ----- Guards that stop the branch from ever running -----

describe('safeParseErrorBody - guards short-circuiting before the branch', () => {
  it('returns undefined when the content-type header is absent', async () => {
    // `new Response(string)` auto-sets `text/plain;charset=UTF-8` on Node 20+,
    // so the header must be removed explicitly to exercise the missing-header
    // path (`res.headers.get('content-type') ?? ''`).
    const res = new Response('{"message":"m"}', { status: 400 });
    res.headers.delete('content-type');
    expect(res.headers.get('content-type')).toBeNull();
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a non-JSON content-type', async () => {
    const res = jsonResponse('{"message":"m"}', { contentType: 'text/plain' });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a vendor +json content-type', async () => {
    const res = jsonResponse('{"message":"m"}', { contentType: 'application/vnd.api+json' });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined when content-type casing does not match application/json', async () => {
    // `String.prototype.includes` is case-sensitive; the header match is too.
    const res = jsonResponse('{"message":"m"}', { contentType: 'Application/JSON' });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for an empty body', async () => {
    expect(await safeParseErrorBody(jsonResponse(''))).toBeUndefined();
  });

  it('returns undefined for a whitespace-only body', async () => {
    expect(await safeParseErrorBody(jsonResponse('   '))).toBeUndefined();
  });

  it('returns undefined for malformed JSON', async () => {
    expect(await safeParseErrorBody(jsonResponse('{"message":'))).toBeUndefined();
    expect(await safeParseErrorBody(jsonResponse('not-json'))).toBeUndefined();
    expect(await safeParseErrorBody(jsonResponse('<html>502</html>'))).toBeUndefined();
  });

  it('returns undefined when the body stream fails to read', async () => {
    const res = jsonResponse('{"message":"m"}');
    vi.spyOn(res, 'text').mockRejectedValue(new Error('stream reset'));
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });
});

// ----- The response body is consumed exactly once -----

describe('safeParseErrorBody - the response body is single-use', () => {
  it('parses on the first call and returns undefined on the second', async () => {
    const res = jsonResponse('{"message":"first"}');
    expect(await safeParseErrorBody(res)).toEqual({ message: 'first' });
    // Re-reading a used body rejects inside the try/catch, so repeat calls yield
    // a deterministic `undefined` instead of an unhandled rejection.
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('marks the response body as used after parsing', async () => {
    const res = jsonResponse('{"message":"m"}');
    expect(res.bodyUsed).toBe(false);
    await safeParseErrorBody(res);
    expect(res.bodyUsed).toBe(true);
  });
});

// ----- Same error contract as observed through the public client surface -----

describe('createStellarBillClient - error body contract exposed to callers', () => {
  function jsonFetch(
    body: unknown,
    init: { status?: number; contentType?: string } = {},
  ): typeof globalThis.fetch {
    const status = init.status ?? 400;
    const contentType = init.contentType ?? 'application/json';
    const text = typeof body === 'string' ? body : JSON.stringify(body);
    return vi.fn(async () => new Response(text, { status, headers: { 'content-type': contentType } }));
  }

  it('surfaces the JSON error envelope on result.error for a non-2xx', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: jsonFetch({ error: 'not_found', message: 'missing', code: 'E_404' }, { status: 404 }),
    });
    const r = await sdk.getSubscription('missing');
    expect(r.status).toBe(404);
    expect(r.data).toBeUndefined();
    expect(r.error).toEqual({ error: 'not_found', message: 'missing', code: 'E_404' });
  });

  it('attaches the parsed body to StellarBillError when throwOnError is set', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: jsonFetch({ message: 'quota exceeded', code: 'E_RATE' }, { status: 429 }),
      throwOnError: true,
    });
    const err = await sdk.getHealth().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(StellarBillError);
    expect(err).toMatchObject({
      status: 429,
      body: { message: 'quota exceeded', code: 'E_RATE' },
    });
  });

  it('resolves rather than throwing for a non-2xx when throwOnError is unset', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: jsonFetch({ message: 'boom' }, { status: 500 }),
    });
    const r = await sdk.getHealth();
    expect(r.status).toBe(500);
    expect(r.error).toEqual({ message: 'boom' });
  });

  it('keeps a non-JSON error body on the generic HTTP fallback path', async () => {
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: jsonFetch('<html>bad gateway</html>', { status: 502, contentType: 'text/html' }),
      throwOnError: true,
    });
    const err = await sdk.getHealth().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(StellarBillError);
    expect(err).toMatchObject({ status: 502 });
    expect((err as StellarBillError).body).toBeUndefined();
    expect((err as StellarBillError).message).toContain('502');
  });
});
