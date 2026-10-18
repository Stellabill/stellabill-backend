import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Boundary coverage for the caller-header normalization loop in
 * `sdks/ts/src/client.ts:172`:
 *
 *   const lower = k.toLowerCase();
 *   if (lower === 'authorization') continue;
 *
 * The guard must match the header name case-insensitively but must NOT
 * over-match names that merely contain or prefix `authorization`. This suite
 * pins the exact-match boundary so a future rewrite (e.g. `startsWith`,
 * `includes`, or a missing `toLowerCase()`) is caught immediately.
 */

type FetchCall = {
  url: string;
  inputHeaders: Record<string, string>;
  initHeaders: Record<string, string>;
};

const BASE = 'https://api.example.com';

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

function headersFromInit(init?: RequestInit): Record<string, string> {
  const out: Record<string, string> = {};
  if (!init?.headers) return out;
  new Headers(init.headers).forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

function callHeaders(call: FetchCall): Record<string, string> {
  return { ...call.initHeaders, ...call.inputHeaders };
}

function mockFetchOnce(body: unknown): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = body === undefined ? '' : JSON.stringify(body);
  // Build a fresh Response per call so repeated requests each get a readable body.
  const makeRes = (): Response =>
    new Response(text, { status: 200, headers: { 'content-type': 'application/json' } });
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return makeRes();
  });
  return { fetch, calls };
}

function authorizationValues(headers: Record<string, string>): string[] {
  return Object.entries(headers)
    .filter(([key]) => key.toLowerCase() === 'authorization')
    .map(([, value]) => value);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('caller header normalization - authorization exact-match boundary (client.ts:172)', () => {
  it.each(['authorization', 'Authorization', 'AUTHORIZATION', 'AuThOrIzAtIoN', 'aUtHoRiZaTiOn'])(
    'drops caller-supplied `%s` regardless of casing',
    async (key) => {
      const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
      const sdk = createStellarBillClient({
        baseUrl: BASE,
        headers: { [key]: 'Bearer attacker-controlled' },
        fetch,
      });
      await sdk.getHealth();

      const headers = callHeaders(calls[0]!);
      expect(authorizationValues(headers)).toEqual([]);
      expect(headers['authorization']).toBeUndefined();
    },
  );

  it.each(['x-authorization', 'authorization-id', 'proxy-authorization', 'x-authorization-token', 'authorization2'])(
    'forwards near-miss header `%s` untouched (the guard is exact-match, not prefix/substring)',
    async (key) => {
      const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
      const sdk = createStellarBillClient({
        baseUrl: BASE,
        headers: { [key]: 'keep-me' },
        fetch,
      });
      await sdk.getHealth();

      const headers = callHeaders(calls[0]!);
      // Keys are lower-cased when normalized, values are preserved verbatim.
      expect(headers[key.toLowerCase()]).toBe('keep-me');
      expect(authorizationValues(headers)).toEqual([]);
    },
  );

  it('drops the caller authorization even when an SDK token is set, and never drops sibling headers', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'sdk-token',
      headers: { authorization: 'Bearer attacker-controlled', 'X-Tenant-Id': 'tenant-1' },
      fetch,
    });
    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    // Exactly one authorization header, and it is the SDK-owned one.
    expect(authorizationValues(headers)).toEqual(['Bearer sdk-token']);
    expect(headers['authorization']).not.toContain('attacker');
    expect(headers['x-tenant-id']).toBe('tenant-1');
  });

  it('drops an empty-valued caller authorization (the guard runs before the value-length filter)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      headers: { authorization: '', 'X-Keep': 'v' },
      fetch,
    });
    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(authorizationValues(headers)).toEqual([]);
    expect(headers['x-keep']).toBe('v');
  });

  it('never emits the caller value across repeated requests', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'sdk-token',
      headers: { Authorization: 'Bearer attacker-controlled' },
      fetch,
    });
    await sdk.getHealth();
    await sdk.getHealth();
    await sdk.getHealth();

    expect(calls).toHaveLength(3);
    for (const call of calls) {
      const headers = callHeaders(call);
      expect(authorizationValues(headers)).toEqual(['Bearer sdk-token']);
    }
  });
});
