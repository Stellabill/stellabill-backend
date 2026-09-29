import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Focused *accepted-input* coverage for `StellarBillClient.getToken()`
 * (`sdks/ts/src/client.ts:30`, implemented at `client.ts:247` as
 * `return tokenHolder.get();`).
 *
 * The branch is a read-only accessor, so the "accepted input" contract is:
 * a well-formed bearer token supplied through the constructor (or `setToken`)
 * is returned verbatim after `sanitizeToken()` normalisation, is never
 * truncated or re-encoded, never gains the `Bearer ` scheme prefix, and is
 * stable across repeated reads.
 *
 * This complements the existing suites: `test/auth.test.ts` (sanitizeToken
 * table), `test/client.test.ts` (rotation + malformed token dropped) and the
 * rejection-focused `getToken()` cases. The cases below pin the *positive*
 * side of the accessor's input domain.
 */

type FetchCall = {
  url: string;
  headers: Record<string, string>;
};

function mockFetch(
  body: unknown,
  init: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), headers });
    // A fresh Response per call: `Body has already been read` otherwise.
    return new Response(text, { status, headers: { 'content-type': contentType } });
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';
const HEALTH = { status: 'ok', service: 'stellarbill-backend' };

afterEach(() => {
  vi.restoreAllMocks();
});

describe('getToken() - accepted tokens are returned verbatim', () => {
  it('returns a single-character token (minimal accepted input)', () => {
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'x', fetch });
    expect(sdk.getToken()).toBe('x');
  });

  it('returns a JWT-shaped token with dots, dashes and underscores unchanged', () => {
    const jwt =
      'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl_-abc';
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: jwt, fetch });
    expect(sdk.getToken()).toBe(jwt);
  });

  it('returns a URL-safe base64 token containing +, / and = characters', () => {
    const token = 'Ab+C/d==';
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token, fetch });
    expect(sdk.getToken()).toBe(token);
  });

  it('does not truncate a long accepted token', () => {
    const token = 't'.repeat(4096);
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token, fetch });
    expect(sdk.getToken()).toBe(token);
    expect(sdk.getToken()).toHaveLength(4096);
  });

  it('does not prefix the returned token with the Bearer scheme', () => {
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'raw-token', fetch });
    const token = sdk.getToken();
    expect(token).toBe('raw-token');
    expect(token?.startsWith('Bearer')).toBe(false);
  });

  it('normalises surrounding whitespace but preserves the accepted token body', () => {
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '  accepted-token  ', fetch });
    expect(sdk.getToken()).toBe('accepted-token');
  });
});

describe('getToken() - read stability for accepted inputs', () => {
  it('returns the same value on every read (accessor has no side effects)', () => {
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'stable', fetch });
    expect(sdk.getToken()).toBe('stable');
    expect(sdk.getToken()).toBe('stable');
    expect(sdk.getToken()).toBe('stable');
  });

  it('is readable before any request is made', () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'pre-flight', fetch });
    expect(calls).toHaveLength(0);
    expect(sdk.getToken()).toBe('pre-flight');
  });

  it('keeps the accepted value across a sequence of accepted rotations', () => {
    const { fetch } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    for (const token of ['one', 'two', 'three']) {
      sdk.setToken(token);
      expect(sdk.getToken()).toBe(token);
    }
  });

  it('is the exact token used on the wire for an accepted input', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'wire-accepted', fetch });
    await sdk.getHealth();
    expect(calls).toHaveLength(1);
    expect(calls[0]!.headers['authorization']).toBe(`Bearer ${sdk.getToken()}`);
  });
});
