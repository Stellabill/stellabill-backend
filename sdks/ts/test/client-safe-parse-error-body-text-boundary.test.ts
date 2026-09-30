import { afterEach, describe, expect, it, vi } from 'vitest';

import { safeParseErrorBody } from '../src/index.js';

/**
 * Boundary coverage for the body-read step of `safeParseErrorBody`
 * (`sdks/ts/src/client.ts:112` — `const text = await res.text();`).
 *
 * The function only reaches that line after the content-type gate, then feeds
 * the result to `JSON.parse`. The observable boundaries are therefore:
 *   - whether the body is read at all (content-type gate),
 *   - the empty-string short-circuit *before* parsing,
 *   - what `JSON.parse` is allowed to accept as an error envelope,
 *   - the case sensitivity of the content-type check.
 *
 * Existing suites already cover missing/non-JSON content types, an empty body,
 * invalid JSON, arrays, strings, null, whitespace-only bodies and the `catch`
 * branch. The cases below cover the remaining edges of this branch.
 */

function jsonResponse(body: string, contentType = 'application/json'): Response {
  return new Response(body, { status: 400, headers: { 'content-type': contentType } });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('safeParseErrorBody - text-read boundary (:112)', () => {
  it('reads the body exactly once per invocation', async () => {
    const res = jsonResponse('{"code":"x"}');
    const spy = vi.spyOn(res, 'text');
    await safeParseErrorBody(res);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('does not read the body when the content type is not JSON', async () => {
    const res = jsonResponse('{"code":"x"}', 'text/plain');
    const spy = vi.spyOn(res, 'text');
    const out = await safeParseErrorBody(res);
    expect(out).toBeUndefined();
    expect(spy).not.toHaveBeenCalled();
  });

  it('short-circuits on an empty body before calling JSON.parse', async () => {
    const res = jsonResponse('');
    const parse = vi.spyOn(JSON, 'parse');
    const out = await safeParseErrorBody(res);
    expect(out).toBeUndefined();
    expect(parse).not.toHaveBeenCalled();
  });

  it('parses a body padded with surrounding whitespace/newlines', async () => {
    const res = jsonResponse('\n  {"message":"padded","code":"p"}  \n');
    await expect(safeParseErrorBody(res)).resolves.toEqual({ message: 'padded', code: 'p' });
  });
});

// Issue #891 — the `text` guard itself (`if (!text) return undefined;`,
// client.ts:133; the issue quotes it as :113, before #895 inserted
// `normalizeErrorBody` above it). The suite above pins the short-circuit for a
// 400; what it does not pin is *where* the rejected input stops. An empty string
// also throws in `JSON.parse`, so every assertion that only checks the return
// value stays green even if the guard is deleted and the body falls through to
// the `catch`. These cases therefore assert which side of the guard the `text`
// lands on, and that the decision depends on the decoded text alone, not on the
// status.
describe('safeParseErrorBody - rejected text input (:133)', () => {
  it('rejects an empty body on a server error without reaching JSON.parse', async () => {
    const res = new Response('', { status: 500, headers: { 'content-type': 'application/json' } });
    const parse = vi.spyOn(JSON, 'parse');

    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();

    expect(parse).not.toHaveBeenCalled();
  });

  it('rejects a body the decoder reduces to empty without reaching JSON.parse', async () => {
    // A lone BOM is stripped while decoding, so `text` is `''` by the time the
    // guard sees it — the rejection happens at :113, not in the parser.
    const res = jsonResponse('\uFEFF');
    const parse = vi.spyOn(JSON, 'parse');

    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();

    expect(parse).not.toHaveBeenCalled();
  });

  it('sends whitespace-only text to JSON.parse, which then rejects it', async () => {
    // Whitespace is truthy, so the guard must not fire. Both paths end in
    // `undefined`, and only the call count tells them apart.
    const res = jsonResponse('   \n\t ');
    const parse = vi.spyOn(JSON, 'parse');

    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();

    expect(parse).toHaveBeenCalledOnce();
  });

  it('sends non-empty text to JSON.parse exactly once and returns the envelope', async () => {
    const body = { message: 'bad', code: 'invalid' };
    const res = jsonResponse(JSON.stringify(body));
    const parse = vi.spyOn(JSON, 'parse');

    await expect(safeParseErrorBody(res)).resolves.toEqual(body);

    expect(parse).toHaveBeenCalledOnce();
  });
});

describe('safeParseErrorBody - accepted JSON shapes', () => {
  it('accepts an empty object as a valid error envelope', async () => {
    const res = jsonResponse('{}');
    await expect(safeParseErrorBody(res)).resolves.toEqual({});
  });

  it('returns a nested object unchanged', async () => {
    const body = { code: 'nested', details: { field: 'id', retryable: true } };
    const res = jsonResponse(JSON.stringify(body));
    await expect(safeParseErrorBody(res)).resolves.toEqual(body);
  });

  it('parses a valid object when content type carries charset without a space', async () => {
    const res = jsonResponse('{"code":"charset"}', 'application/json;charset=utf-8');
    await expect(safeParseErrorBody(res)).resolves.toEqual({ code: 'charset' });
  });

  it('does not let a __proto__ key pollute Object.prototype', async () => {
    const res = jsonResponse('{"__proto__":{"polluted":true},"code":"proto"}');
    const parsed = await safeParseErrorBody(res);
    expect(parsed?.code).toBe('proto');
    // JSON.parse defines `__proto__` as an own data property; it must not
    // have been applied as the object's prototype.
    expect(Object.getPrototypeOf(parsed)).toBe(Object.prototype);
    expect(({} as Record<string, unknown>)['polluted']).toBeUndefined();
  });
});

describe('safeParseErrorBody - rejected scalar boundaries', () => {
  it('returns undefined for the JSON number zero', async () => {
    await expect(safeParseErrorBody(jsonResponse('0'))).resolves.toBeUndefined();
  });

  it('returns undefined for negative zero', async () => {
    await expect(safeParseErrorBody(jsonResponse('-0'))).resolves.toBeUndefined();
  });

  it('is case-sensitive about the content-type gate', async () => {
    // `contentType.includes('application/json')` is a case-sensitive match.
    // HTTP media types are case-insensitive, so this pins the current
    // behaviour as an explicit regression guard rather than a silent change.
    const res = jsonResponse('{"code":"upper"}', 'Application/JSON');
    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
  });
});
