import { describe, expect, it } from 'vitest';

import { createStellarBillClient, safeParseErrorBody } from '../src/index.js';

/**
 * Boundary coverage for the `return undefined` branch in `safeParseErrorBody`
 * (`sdks/ts/src/client.ts:118`).
 *
 * The helper has three ways to produce `undefined` and callers must not
 * distinguish them, because the value feeds straight into
 * `makeErrorMessage`'s `body?.message ?? body?.error ?? "HTTP <status>"` chain:
 *
 *  - line 110 - the response is not advertised as JSON;
 *  - line 113 - the body is empty;
 *  - line 118 - the JSON parsed, but to something that is not a plain object;
 *  - line 120 - parsing/reading threw.
 *
 * This suite exercises all four, plus the accepted shapes, so a regression that
 * starts surfacing arrays or scalars as `ApiErrorBody` is caught immediately.
 */

const json = (body: string, contentType = 'application/json'): Response =>
  new Response(body, { headers: { 'content-type': contentType } });

describe('safeParseErrorBody - not advertised as JSON (line 110)', () => {
  it.each([
    ['text/plain', 'text/plain'],
    ['text/html', 'text/html'],
    ['application/xml', 'application/xml'],
    ['application/ld+json', 'application/ld+json'],
    ['an empty content type', ''],
  ])('returns undefined for %s', async (_label, contentType) => {
    const res = new Response('{"message":"x"}', { headers: { 'content-type': contentType } });

    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
  });

  it('accepts a JSON content type carrying a charset parameter', async () => {
    const res = json('{"message":"x"}', 'application/json; charset=utf-8');

    await expect(safeParseErrorBody(res)).resolves.toEqual({ message: 'x' });
  });

  it('returns undefined when the response has no content-type header at all', async () => {
    const res = new Response('{"message":"x"}');

    await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
  });
});

describe('safeParseErrorBody - empty body (line 113)', () => {
  it('returns undefined for a zero-length body', async () => {
    await expect(safeParseErrorBody(json(''))).resolves.toBeUndefined();
  });

  it('returns undefined for a whitespace-only body only when it is not valid JSON', async () => {
    // A single space is not valid JSON, so this is the catch path; a body of
    // "null" parses and is rejected at line 118 instead.
    await expect(safeParseErrorBody(json(' '))).resolves.toBeUndefined();
  });
});

describe('safeParseErrorBody - parsed to a non-plain-object (line 118)', () => {
  it.each([
    ['an array of error objects', '[{"message":"nope"}]'],
    ['an empty array', '[]'],
    ['the literal null', 'null'],
    ['the literal false', 'false'],
    ['the literal true', 'true'],
    ['a number', '12'],
    ['a negative float', '-1.5'],
    ['a quoted string', '"nope"'],
    ['an empty quoted string', '""'],
  ])('returns undefined for %s', async (_label, body) => {
    await expect(safeParseErrorBody(json(body))).resolves.toBeUndefined();
  });

  it('returns an empty object for "{}" rather than undefined', async () => {
    // This is the boundary the fallback chain depends on: the object is kept so
    // that `body?.message` is `undefined` rather than the whole body being lost.
    await expect(safeParseErrorBody(json('{}'))).resolves.toEqual({});
  });

  it('keeps a nested object value instead of rejecting the envelope', async () => {
    const body = '{"message":"x","details":{"field":"cursor"}}';

    await expect(safeParseErrorBody(json(body))).resolves.toEqual({
      message: 'x',
      details: { field: 'cursor' },
    });
  });

  it('keeps an array-valued property on an otherwise valid object', async () => {
    const body = '{"message":"x","fields":["cursor","limit"]}';

    await expect(safeParseErrorBody(json(body))).resolves.toEqual({
      message: 'x',
      fields: ['cursor', 'limit'],
    });
  });
});

describe('safeParseErrorBody - parse or read failure (line 120)', () => {
  it('returns undefined for truncated JSON', async () => {
    await expect(safeParseErrorBody(json('{"message":'))).resolves.toBeUndefined();
  });

  it('returns the parsed object for a BOM-prefixed body', async () => {
    // `Response.text()` decodes with a TextDecoder that strips a leading BOM,
    // so the payload reaches `JSON.parse` clean.
    await expect(safeParseErrorBody(json('\uFEFF{"message":"x"}'))).resolves.toEqual({
      message: 'x',
    });
  });

  it('returns undefined when the body is a lone BOM', async () => {
    // The only character is stripped by the decoder, leaving an empty string.
    await expect(safeParseErrorBody(json('\uFEFF'))).resolves.toBeUndefined();
  });

  it('returns undefined when reading the body throws', async () => {
    const broken = {
      headers: new Headers({ 'content-type': 'application/json' }),
      text: async () => {
        throw new Error('stream closed');
      },
    } as unknown as Response;

    await expect(safeParseErrorBody(broken)).resolves.toBeUndefined();
  });

  it('never rejects, whatever the body contains', async () => {
    const bodies = ['{', '}', 'undefined', 'NaN', '{"a":}', '[1,2', '\u0000'];

    for (const body of bodies) {
      await expect(safeParseErrorBody(json(body))).resolves.toBeUndefined();
    }
  });
});

describe('safeParseErrorBody - integration with a rejected request', () => {
  async function resultFor(body: string, contentType = 'application/json') {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      fetch: (async () => new Response(body, { status: 400, headers: { 'content-type': contentType } })) as unknown as typeof globalThis.fetch,
    });
    return client.getHealth();
  }

  it('exposes the parsed object as result.error for a JSON object body', async () => {
    const result = await resultFor('{"message":"bad request","code":"invalid"}');

    expect(result.status).toBe(400);
    expect(result.error).toEqual({ message: 'bad request', code: 'invalid' });
  });

  it('leaves result.error undefined for the scalar shapes openapi-fetch rejects', async () => {
    for (const body of ['null', '3', 'true', '"x"', '{"message":', '']) {
      const result = await resultFor(body);
      expect(result.error).toBeUndefined();
    }
  });

  it('passes a JSON array straight through as result.error', async () => {
    // `wrap()` only checks `typeof error === 'object'`, so unlike
    // `safeParseErrorBody` it does not reject arrays. The envelope still works
    // because an array has no `message`/`error` key, so the detail falls back
    // to the HTTP status.
    const result = await resultFor('[{"message":"x"}]');

    expect(result.error).toEqual([{ message: 'x' }]);
  });

  it('parses a JSON body even when the response advertises text/plain', async () => {
    // openapi-fetch sniffs the payload rather than trusting the content type,
    // so the SDK's envelope carries an object for a mislabelled response.
    const result = await resultFor('{"message":"x"}', 'text/plain');

    expect(result.error).toEqual({ message: 'x' });
  });
});
