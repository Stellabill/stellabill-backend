/**
 * Rejected / boundary-input coverage for `safeParseErrorBody()` — in particular
 * the `return undefined;` catch reached at `sdks/ts/src/client.ts:120`.
 *
 * The existing `describe('safeParseErrorBody')` block in `client.test.ts` already
 * covers the content-type gate (missing/non-JSON), the empty body, a valid
 * object, invalid JSON, string/array/null values, and `text()` throwing.
 *
 * This file covers the remaining genuine gaps:
 *  - whitespace-only text, which is non-empty yet still reaches the JSON.parse
 *    catch at :120,
 *  - a `charset` parameter on the content type (must not break detection),
 *  - the non-object JSON primitives `true` / `false` / `42`,
 *  - an object made only of unknown fields (returned verbatim),
 *  - a UTF-8 BOM prefix on the body.
 */
import { describe, expect, it } from 'vitest';

import { safeParseErrorBody } from '../src/index.js';

function jsonResponse(body: string, contentType = 'application/json'): Response {
  return new Response(body, { status: 400, headers: { 'content-type': contentType } });
}

describe('safeParseErrorBody - rejected inputs reach the parse catch (client.ts:120)', () => {
  it('returns undefined for a whitespace-only body', async () => {
    // Whitespace is truthy, so the `if (!text) return undefined;` guard does NOT
    // fire; JSON.parse('   ') throws and the catch at :120 returns undefined.
    const r = jsonResponse('   ');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for a whitespace-only body made of tabs and newlines', async () => {
    const r = jsonResponse('\n\t  \r\n');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for the JSON boolean true', async () => {
    const r = jsonResponse('true');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for the JSON boolean false', async () => {
    const r = jsonResponse('false');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for a JSON number', async () => {
    const r = jsonResponse('42');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});

describe('safeParseErrorBody - content-type charset parameter', () => {
  it('parses a valid object when the content type carries charset=utf-8', async () => {
    const r = jsonResponse(
      JSON.stringify({ message: 'bad', code: 'x' }),
      'application/json; charset=utf-8',
    );
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bad', code: 'x' });
  });

  it('returns undefined when the content type carries charset but the JSON is invalid', async () => {
    const r = jsonResponse('{"message": ', 'application/json; charset=utf-8');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});

describe('safeParseErrorBody - object shape', () => {
  it('returns an object containing only unknown fields as-is', async () => {
    const r = jsonResponse(JSON.stringify({ unexpected: 1, nested: { a: true } }));
    expect(await safeParseErrorBody(r)).toEqual({ unexpected: 1, nested: { a: true } });
  });
});

describe('safeParseErrorBody - UTF-8 BOM', () => {
  it('parses a BOM-prefixed JSON body because Response.text() strips the BOM', async () => {
    // Observed behavior: the WHATWG "UTF-8 decode" performed by Response.text()
    // removes a leading U+FEFF, so the BOM never reaches JSON.parse (which would
    // otherwise throw on it). A BOM-prefixed error body therefore yields the
    // parsed object rather than undefined. Asserting it locks that contract.
    const r = jsonResponse('\ufeff{"message":"bom"}');
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bom' });
  });
});
