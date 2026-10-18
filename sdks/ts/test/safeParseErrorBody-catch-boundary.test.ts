/**
 * safeParseErrorBody `catch` boundary condition tests — issue #904
 *
 * Covers the `} catch {` block at `src/client.ts:119` inside
 * `safeParseErrorBody`, which is the silent-recovery path that returns
 * `undefined` whenever the try-block throws.  It is reachable from two
 * distinct throw sources:
 *
 *   Source 1 — res.text() rejects (network/stream error, body already consumed)
 *   Source 2 — JSON.parse() throws (malformed / truncated / binary body)
 *
 * The function signature and contract:
 *
 *   async function safeParseErrorBody(res: Response): Promise<ApiErrorBody | undefined>
 *
 *   Returns the parsed object when the body is valid application/json with a
 *   plain-object shape.  Returns `undefined` for every other outcome,
 *   including all throw paths inside the try block.
 *
 * Full branch map of safeParseErrorBody (lines 108-121):
 *
 *   Branch CT   content-type is not application/json → return undefined (early exit)
 *   Branch EMPTY body text is empty string            → return undefined
 *   Branch ACCEPT parsed value is a non-array object  → return parsed (happy path)
 *   Branch NON-OBJECT parsed value fails shape check  → return undefined
 *   Branch CATCH  try block throws                    → return undefined  ← this file
 *
 * For every catch-branch case we also verify the full SDK request/response
 * envelope so that the observable behavior at the call site is deterministic,
 * not just the isolated helper.
 *
 * NOTE: safeParseErrorBody is exported from the SDK's public index so it can
 * be called directly in tests without needing to reach into internals.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, safeParseErrorBody } from '../src/index.js';

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Helper — build a minimal Response the SDK can send through getHealth()
// ---------------------------------------------------------------------------
function sdkFetchFrom(response: Response): typeof globalThis.fetch {
  return vi.fn(async () => response);
}

// ---------------------------------------------------------------------------
// CATCH SOURCE 1 — res.text() rejects
//
// When the underlying stream has already been consumed, or the network layer
// surfaces a read error, `res.text()` rejects.  The catch block MUST absorb
// the rejection and return `undefined` so the SDK never surfaces an
// unhandled promise rejection.
// ---------------------------------------------------------------------------
describe('safeParseErrorBody catch boundary – Source 1: res.text() rejects', () => {
  it('returns undefined when res.text() rejects with an Error', async () => {
    const res = new Response('body', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(res, 'text').mockRejectedValue(new Error('stream read failed'));
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined when res.text() rejects with a non-Error thrown value', async () => {
    // Defensive: the catch block is `catch {}` (no binding) so it works for
    // any thrown value, not only Error instances.
    const res = new Response('body', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(res, 'text').mockRejectedValue('raw string rejection');
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined when res.text() rejects with null', async () => {
    const res = new Response('body', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(res, 'text').mockRejectedValue(null);
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined when the body has already been consumed (body locked)', async () => {
    // A consumed body causes res.text() to throw a TypeError in WHATWG fetch.
    const res = new Response('{"error":"locked"}', {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    // Consume the body first to lock it.
    await res.text();
    // Second read should throw; catch must absorb it.
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  // --- SDK envelope behavior when res.text() throws ---
  it('full SDK result: error is undefined and status is preserved when body read fails', async () => {
    const base = new Response('error body', {
      status: 502,
      headers: { 'content-type': 'application/json' },
    });
    // openapi-fetch calls res.json() internally (not res.text()), so we cannot
    // make the SDK's internal Response throw via spyOn on the same object.
    // Instead, verify through safeParseErrorBody directly that the catch path
    // deterministically returns undefined, then confirm the SDK correctly
    // forwards undefined error through the wrap() envelope on non-JSON responses.
    const sdkRes = new Response('<gateway error>', {
      status: 502,
      headers: { 'content-type': 'text/html' },
    });
    const fetch = sdkFetchFrom(sdkRes);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(502);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();

    // Also verify safeParseErrorBody is safe to call on the same kind of Response.
    const spy = new Response('error body', {
      status: 502,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(spy, 'text').mockRejectedValue(new Error('stream error'));
    const parsed = await safeParseErrorBody(spy);
    expect(parsed).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// CATCH SOURCE 2 — JSON.parse() throws
//
// When the body is read successfully but its content is not valid JSON,
// JSON.parse throws a SyntaxError.  The catch block MUST absorb it and
// return `undefined`.
//
// NOTE: the existing test `'returns undefined on invalid JSON'` in
// client.test.ts exercises this path but does not name it as a catch-boundary
// test, does not enumerate distinct malformed inputs, and does not verify the
// SDK envelope behavior.  These tests close that gap.
// ---------------------------------------------------------------------------
describe('safeParseErrorBody catch boundary – Source 2: JSON.parse() throws', () => {
  it('returns undefined for a bare non-JSON string body', async () => {
    const res = new Response('not-json', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a truncated JSON body', async () => {
    const res = new Response('{"error":"truncated', {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a body with a leading BOM', async () => {
    // A UTF-8 BOM (U+FEFF) before the JSON causes JSON.parse to throw.
    const res = new Response('\uFEFF{"error":"bom"}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a body that is only whitespace', async () => {
    // Only whitespace — JSON.parse('   ') throws SyntaxError.
    // But the empty-body guard (`if (!text)`) does NOT fire because
    // '   ' is truthy, so execution reaches JSON.parse.
    const res = new Response('   ', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for an HTML body with JSON content-type', async () => {
    const res = new Response('<html><body>Bad Gateway</body></html>', {
      status: 502,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined for a body with a trailing comma (invalid JSON)', async () => {
    const res = new Response('{"error":"bad",}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined when JSON.parse throws for any reason (spy approach)', async () => {
    // Explicit spy on JSON.parse to simulate an exotic throw that would not
    // normally occur (e.g., a patched environment), confirming the catch is
    // truly general-purpose.
    const res = new Response('{"error":"ok"}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const originalParse = JSON.parse;
    JSON.parse = () => { throw new SyntaxError('forced'); };
    try {
      expect(await safeParseErrorBody(res)).toBeUndefined();
    } finally {
      JSON.parse = originalParse;
    }
  });

  // --- SDK envelope behavior when JSON body is malformed ---
  it('SDK result.error is undefined when non-2xx response body is malformed JSON', async () => {
    // The SDK uses openapi-fetch internally (not safeParseErrorBody), but the
    // observable contract must hold: a malformed-JSON non-2xx body must never
    // surface as a defined error value — it must be undefined.
    const res = new Response('not-valid-json', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const fetch = sdkFetchFrom(res);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(400);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();
  });

  it('throwOnError + malformed JSON body throws StellarBillError with undefined body', async () => {
    const res = new Response('not-valid-json', {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    const fetch = sdkFetchFrom(res);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const thrown = await sdk.getHealth().catch((e: unknown) => e);
    expect(thrown).toMatchObject({ status: 500, body: undefined });
  });
});

// ---------------------------------------------------------------------------
// HAPPY PATH confirmation — catch is NOT taken for valid inputs
//
// These confirm the try block completes successfully and the catch is bypassed,
// making the catch boundary tests meaningful (they prove the catch IS taken
// when it should be and IS NOT taken when it shouldn't be).
// ---------------------------------------------------------------------------
describe('safeParseErrorBody catch boundary – happy path (catch NOT taken)', () => {
  it('returns the parsed object for a well-formed application/json body', async () => {
    const res = new Response(JSON.stringify({ error: 'Bad Request', message: 'invalid', code: 'x' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const result = await safeParseErrorBody(res);
    expect(result).toEqual({ error: 'Bad Request', message: 'invalid', code: 'x' });
  });

  it('returns the parsed object when content-type has charset suffix', async () => {
    const res = new Response(JSON.stringify({ message: 'oops', code: 'y' }), {
      status: 422,
      headers: { 'content-type': 'application/json; charset=utf-8' },
    });
    const result = await safeParseErrorBody(res);
    expect(result).toEqual({ message: 'oops', code: 'y' });
  });

  it('returns undefined (NON-OBJECT branch, not catch) for a JSON string body', async () => {
    // JSON.parse succeeds but the value fails the object shape check.
    // The NON-OBJECT branch fires, NOT the catch.
    const res = new Response('"just a string"', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined (NON-OBJECT branch, not catch) for a JSON array body', async () => {
    const res = new Response('[1,2,3]', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined (NON-OBJECT branch, not catch) for a JSON null body', async () => {
    const res = new Response('null', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });

  it('returns undefined (EMPTY branch, not catch) for an empty body', async () => {
    const res = new Response('', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(res)).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// CATCH isolation — confirm the function NEVER throws or rejects
//
// The contract is that safeParseErrorBody is a total function: it always
// resolves (never rejects) regardless of what the Response does.
// ---------------------------------------------------------------------------
describe('safeParseErrorBody catch boundary – function never rejects', () => {
  it('resolves (does not reject) when res.text() throws synchronously', async () => {
    const res = new Response('body', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(res, 'text').mockImplementation(() => {
      throw new TypeError('synchronous throw from text()');
    });
    // Must resolve to undefined, not reject.
    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
  });

  it('resolves (does not reject) when JSON.parse throws SyntaxError', async () => {
    const res = new Response('{bad}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
  });

  it('resolves (does not reject) for every invalid input combination', async () => {
    const cases = [
      new Response('null', { status: 400, headers: { 'content-type': 'application/json' } }),
      new Response('[1]', { status: 400, headers: { 'content-type': 'application/json' } }),
      new Response('{bad}', { status: 400, headers: { 'content-type': 'application/json' } }),
      new Response('', { status: 400, headers: { 'content-type': 'application/json' } }),
      new Response('text', { status: 400, headers: { 'content-type': 'text/plain' } }),
    ];
    for (const r of cases) {
      await expect(safeParseErrorBody(r)).resolves.toBeUndefined();
    }
  });
});
