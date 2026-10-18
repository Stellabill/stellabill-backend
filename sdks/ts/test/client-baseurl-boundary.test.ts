import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

/**
 * Boundary coverage for `validateBaseUrl` in `sdks/ts/src/client.ts:87`:
 *
 *   if (typeof raw !== 'string' || raw.trim().length === 0) {
 *     throw new StellarBillConfigError('baseUrl must be a non-empty string');
 *   }
 *
 * The guard sits between the "required" branch (`undefined` / `null`) and the
 * URL-parse branch. This suite pins every value that lands on either side of
 * it: non-string types, empty strings, whitespace-only strings (including
 * Unicode whitespace) and boxed String objects must all fail here — not pass
 * through to `new URL()`. It also pins that the trim is only an emptiness
 * check, so a valid URL padded with surrounding whitespace is still accepted.
 */

const BASE = 'https://api.example.com';

const noopFetch: typeof globalThis.fetch = vi.fn(
  async () => new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } }),
) as unknown as typeof globalThis.fetch;

type FetchCall = { url: string };

function mockFetchOnce(body: unknown): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = JSON.stringify(body);
  const makeRes = (): Response =>
    new Response(text, { status: 200, headers: { 'content-type': 'application/json' } });
  const fetch: typeof globalThis.fetch = vi.fn(async (input) => {
    calls.push({ url: typeof input === 'string' ? input : input instanceof Request ? input.url : String(input) });
    return makeRes();
  });
  return { fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('validateBaseUrl - required vs non-empty boundary (client.ts:87)', () => {
  it('throws the distinct "required" error for undefined and null', () => {
    expect(() => createStellarBillClient({ baseUrl: undefined as unknown as string, fetch: noopFetch })).toThrow(
      StellarBillConfigError,
    );
    expect(() => createStellarBillClient({ baseUrl: undefined as unknown as string, fetch: noopFetch })).toThrow(
      /baseUrl is required/,
    );
    expect(() => createStellarBillClient({ baseUrl: null as unknown as string, fetch: noopFetch })).toThrow(
      /baseUrl is required/,
    );
  });

  it.each<[string, unknown]>([
    ['number 0', 0],
    ['number', 42],
    ['NaN', Number.NaN],
    ['boolean false', false],
    ['boolean true', true],
    ['array', []],
    ['plain object', {}],
    ['function', () => BASE],
    ['symbol', Symbol('baseUrl')],
  ])('rejects non-string baseUrl (%s) with the non-empty-string error', (_label, value) => {
    expect(() => createStellarBillClient({ baseUrl: value as unknown as string, fetch: noopFetch })).toThrow(
      StellarBillConfigError,
    );
    expect(() => createStellarBillClient({ baseUrl: value as unknown as string, fetch: noopFetch })).toThrow(
      /non-empty string/,
    );
  });

  it.each([['empty string', ''], ['spaces', '   '], ['tab', '\t'], ['newline', '\n'], ['CRLF', '\r\n'], ['mixed whitespace', ' \t\n '], ['non-breaking space', '\u00A0'], ['ideographic space', '\u3000']])(
    'rejects whitespace-only baseUrl (%s) with the non-empty-string error',
    (_label, value) => {
      expect(() => createStellarBillClient({ baseUrl: value, fetch: noopFetch })).toThrow(/non-empty string/);
    },
  );

  it('rejects a boxed String object (typeof object) with the non-empty-string error', () => {
    const boxed = new String(BASE) as unknown as string;
    expect(() => createStellarBillClient({ baseUrl: boxed, fetch: noopFetch })).toThrow(/non-empty string/);
  });

  it('boundary: a single non-whitespace char clears the emptiness check and then fails URL parsing', () => {
    expect(() => createStellarBillClient({ baseUrl: 'x', fetch: noopFetch })).toThrow(/is not a valid URL/);
    expect(() => createStellarBillClient({ baseUrl: 'x', fetch: noopFetch })).not.toThrow(/non-empty string/);
  });

  it('accepts a valid URL padded with surrounding whitespace (trim is only an emptiness check)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: '  https://api.example.com  ', fetch });
    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(calls[0]!.url.startsWith('https://api.example.com/api/health')).toBe(true);
  });
});
