/**
 * Accepted-input / boundary coverage for `safeParseErrorBody()` (client.ts:113 —
 * `if (!text) return undefined;`).
 *
 * The function is exported for callers that validate response shapes, so its
 * accept/reject contract matters: which bodies are parsed, which content types
 * are recognised, and why a body can be non-empty yet still yield `undefined`.
 */
import { describe, expect, it } from 'vitest';

import { safeParseErrorBody } from '../src/index.js';

function jsonResponse(body: string, contentType = 'application/json'): Response {
  return new Response(body, { status: 400, headers: { 'content-type': contentType } });
}

describe('safeParseErrorBody — accepted text', () => {
  it('parses a non-empty JSON object body', async () => {
    const r = jsonResponse(JSON.stringify({ message: 'bad', code: 'x' }));
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bad', code: 'x' });
  });

  it('parses a body with surrounding whitespace', async () => {
    const r = jsonResponse('  \n{ "message": "padded" }\t ');
    expect(await safeParseErrorBody(r)).toEqual({ message: 'padded' });
  });

  it('parses when the content type carries a charset parameter', async () => {
    const r = jsonResponse('{"code":"x"}', 'application/json; charset=utf-8');
    expect(await safeParseErrorBody(r)).toEqual({ code: 'x' });
  });

  it('parses a nested object body', async () => {
    const r = jsonResponse(JSON.stringify({ error: 'nested', details: { field: 'cursor' } }));
    const parsed = await safeParseErrorBody(r);
    expect(parsed?.error).toBe('nested');
    expect((parsed as { details: { field: string } }).details.field).toBe('cursor');
  });

  it('returns an empty object for an empty JSON object body', async () => {
    const r = jsonResponse('{}');
    expect(await safeParseErrorBody(r)).toEqual({});
  });
});

describe('safeParseErrorBody — text rejected by the parser or filter', () => {
  it('returns undefined for a whitespace-only body (non-empty but not JSON)', async () => {
    const r = jsonResponse('   ');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for a JSON number body', async () => {
    const r = jsonResponse('42');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for a JSON array body', async () => {
    const r = jsonResponse('[1,2,3]');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('parses a BOM-prefixed JSON body (the Fetch body decoder strips the BOM)', async () => {
    // The UTF-8 BOM never reaches JSON.parse: Response.text() removes it while
    // decoding, so the body is treated as ordinary JSON.
    const r = jsonResponse('\ufeff{"message":"bom"}');
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bom' });
  });

  it('returns undefined for malformed JSON', async () => {
    const r = jsonResponse('{"message": ');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});

describe('safeParseErrorBody — content-type gate', () => {
  it('returns undefined for a non-JSON content type', async () => {
    const r = jsonResponse('{"message":"x"}', 'text/plain');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined for application/problem+json (not a substring match)', async () => {
    const r = jsonResponse('{"message":"x"}', 'application/problem+json');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('is case-sensitive on the content type', async () => {
    const r = jsonResponse('{"message":"x"}', 'Application/JSON');
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when the content type is absent', async () => {
    const r = new Response('{"message":"x"}', { status: 400 });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});
