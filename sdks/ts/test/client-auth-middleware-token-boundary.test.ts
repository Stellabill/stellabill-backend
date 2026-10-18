import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Focused boundary coverage for the auth middleware's `hasToken()` guard
 * (`sdks/ts/src/client.ts:200` — `if (tokenHolder.hasToken()) { ... }`).
 *
 * `hasToken()` is `typeof token === 'string' && token.length > 0`. Because the
 * middleware runs on *every* request, the observable contract is: the
 * `Authorization` header is present if and only if the holder currently holds
 * a non-empty (already sanitized) token, and it is never re-added from a stale
 * value once the token is cleared.
 *
 * The existing suite covers "no token" and "malformed constructor token is
 * dropped". The boundary cases below exercise the exact edges of `hasToken()`
 * — empty string, whitespace-only, length one — plus rotation across the
 * boundary and auth-bypass prevention while the guard is false.
 */

type FetchCall = {
  url: string;
  headers: Record<string, string>;
};

function mockFetch(
  body: unknown,
  init: { status?: number } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const text = body === undefined ? '' : JSON.stringify(body);
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), headers });
    // A fresh Response per call: `Body has already been read` otherwise.
    return new Response(text, { status, headers: { 'content-type': 'application/json' } });
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';
const HEALTH = { status: 'ok', service: 'stellarbill-backend' };

afterEach(() => {
  vi.restoreAllMocks();
});

describe('auth middleware - hasToken() boundary (:200)', () => {
  it('omits Authorization when no token was ever configured', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('omits Authorization when the token is the empty string', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('');
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('omits Authorization when the token is whitespace-only', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('     ');
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('omits Authorization when the token sanitizes away (inner whitespace)', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('has inner space');
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('emits Authorization for the length-one token on the accepted side of the boundary', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('z');
    expect(sdk.getToken()).toBe('z');
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBe('Bearer z');
  });

  it('never emits an empty or scheme-only Authorization value', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('');
    await sdk.getHealth();
    sdk.setToken('real');
    await sdk.getHealth();
    for (const call of calls) {
      expect(call.headers['authorization']).not.toBe('');
      expect(call.headers['authorization']).not.toBe('Bearer');
      expect(call.headers['authorization']).not.toBe('Bearer ');
    }
  });
});

describe('auth middleware - token rotation across the boundary', () => {
  it('adds and removes Authorization as the holder crosses hasToken()', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    await sdk.getHealth(); // absent
    sdk.setToken('rotated');
    await sdk.getHealth(); // present
    sdk.setToken('');
    await sdk.getHealth(); // absent again

    expect(calls).toHaveLength(3);
    expect(calls[0]!.headers['authorization']).toBeUndefined();
    expect(calls[1]!.headers['authorization']).toBe('Bearer rotated');
    expect(calls[2]!.headers['authorization']).toBeUndefined();
  });

  it('does not replay a cleared token on a later request', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'first', fetch });
    await sdk.getHealth();
    sdk.setToken(undefined);
    await sdk.getHealth();
    await sdk.getHealth();

    expect(calls[0]!.headers['authorization']).toBe('Bearer first');
    expect(calls[1]!.headers['authorization']).toBeUndefined();
    expect(calls[2]!.headers['authorization']).toBeUndefined();
  });

  it('keeps getToken() and header presence in agreement at every boundary', async () => {
    const tokens = [undefined, '', '   ', 'x', 'ok', undefined] as (string | undefined)[];
    for (const token of tokens) {
      const { fetch, calls } = mockFetch(HEALTH);
      const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
      sdk.setToken(token as string);
      await sdk.getHealth();
      const held = sdk.getToken();
      const header = calls[0]!.headers['authorization'];
      if (held === undefined) {
        expect(header).toBeUndefined();
      } else {
        expect(header).toBe(`Bearer ${held}`);
      }
    }
  });
});

describe('auth middleware - auth-bypass prevention while hasToken() is false', () => {
  it('ignores a caller-supplied Authorization header when no token is held', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      headers: { Authorization: 'Bearer attacker-controlled' },
      fetch,
    });
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('ignores a caller-supplied Authorization header after the token is cleared', async () => {
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'legit',
      headers: { Authorization: 'Bearer attacker-controlled' },
      fetch,
    });
    await sdk.getHealth();
    sdk.setToken('');
    await sdk.getHealth();

    expect(calls[0]!.headers['authorization']).toBe('Bearer legit');
    expect(calls[1]!.headers['authorization']).toBeUndefined();
  });
});
