import { describe, expect, it } from 'vitest';

import { safeParseErrorBody } from '../src/index.js';

/**
 * Dedicated rejected-input coverage for the `contentType` gate in
 * `sdks/ts/src/client.ts`:
 *
 * ```ts
 * if (!contentType.includes('application/json')) return undefined;
 * ```
 *
 * The existing suite covers the plain-text / empty / malformed cases. This file
 * pins the parameters, the case-sensitivity and the substring semantics of the
 * guard so a future refactor cannot silently change which responses the SDK is
 * willing to parse as an error envelope.
 */

function response(
  body: string,
  contentType: string | undefined,
  status = 400,
): Response {
  const headers = contentType === undefined ? undefined : { 'content-type': contentType };
  return new Response(body, { status, headers });
}

const ENVELOPE = '{"message":"rate limited","code":"rate_limited"}';

describe('safeParseErrorBody content-type gate', () => {
  it('returns undefined when the content-type header is absent', async () => {
    expect(await safeParseErrorBody(response(ENVELOPE, undefined))).toBeUndefined();
  });

  it('returns undefined for an empty content-type header', async () => {
    expect(await safeParseErrorBody(response(ENVELOPE, ''))).toBeUndefined();
  });

  it('rejects text/html even when the body is valid JSON', async () => {
    expect(await safeParseErrorBody(response(ENVELOPE, 'text/html'))).toBeUndefined();
  });

  it('accepts a JSON content type carrying a charset parameter', async () => {
    const parsed = await safeParseErrorBody(
      response(ENVELOPE, 'application/json; charset=utf-8'),
    );

    expect(parsed).toEqual({ message: 'rate limited', code: 'rate_limited' });
  });

  it('accepts a JSON content type with no space before the charset parameter', async () => {
    const parsed = await safeParseErrorBody(response(ENVELOPE, 'application/json;charset=utf-8'));

    expect(parsed).toEqual({ message: 'rate limited', code: 'rate_limited' });
  });

  it('rejects media types whose casing breaks the case-sensitive match', async () => {
    expect(await safeParseErrorBody(response(ENVELOPE, 'Application/JSON'))).toBeUndefined();
    expect(await safeParseErrorBody(response(ENVELOPE, 'APPLICATION/JSON'))).toBeUndefined();
  });

  it('rejects vendor +json media types that do not contain "application/json"', async () => {
    expect(
      await safeParseErrorBody(response(ENVELOPE, 'application/problem+json')),
    ).toBeUndefined();
  });

  it('accepts any content type that merely contains the JSON token (substring match)', async () => {
    expect(await safeParseErrorBody(response(ENVELOPE, 'text/application/json'))).toEqual({
      message: 'rate limited',
      code: 'rate_limited',
    });
    expect(
      await safeParseErrorBody(response(ENVELOPE, 'application/json-patch+json')),
    ).toEqual({ message: 'rate limited', code: 'rate_limited' });
    expect(await safeParseErrorBody(response(ENVELOPE, 'application/json-seq'))).toEqual({
      message: 'rate limited',
      code: 'rate_limited',
    });
  });

  it('tolerates surrounding whitespace in the content-type value', async () => {
    const parsed = await safeParseErrorBody(response(ENVELOPE, '  application/json  '));

    expect(parsed).toEqual({ message: 'rate limited', code: 'rate_limited' });
  });

  it('returns undefined for a matching type with an empty body', async () => {
    expect(await safeParseErrorBody(response('', 'application/json'))).toBeUndefined();
  });

  it('returns undefined when the body is not valid JSON despite a matching type', async () => {
    expect(await safeParseErrorBody(response('<html>oops</html>', 'application/json'))).toBeUndefined();
  });

  it('returns undefined for JSON primitives and arrays', async () => {
    expect(await safeParseErrorBody(response('"just a string"', 'application/json'))).toBeUndefined();
    expect(await safeParseErrorBody(response('[1,2,3]', 'application/json'))).toBeUndefined();
    expect(await safeParseErrorBody(response('null', 'application/json'))).toBeUndefined();
  });

  it('passes through a nested error envelope without reshaping it', async () => {
    const body = '{"error":"validation","message":"bad input","code":"invalid","details":{"field":"email"}}';

    const parsed = await safeParseErrorBody(response(body, 'application/json'));

    expect(parsed).toEqual({
      error: 'validation',
      message: 'bad input',
      code: 'invalid',
      details: { field: 'email' },
    });
  });

  it('never throws across the rejected/unknown content-type matrix', async () => {
    const cases: Array<string | undefined> = [
      undefined,
      '',
      'text/plain',
      'text/xml',
      'application/octet-stream',
      'application/*',
      'text/json',
      'multipart/form-data; boundary=x',
    ];

    for (const contentType of cases) {
      await expect(safeParseErrorBody(response(ENVELOPE, contentType))).resolves.toBeUndefined();
    }
  });
});
