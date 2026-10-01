import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

type FetchCall = {
  url: string;
  inputHeaders: Record<string, string>;
  initHeaders: Record<string, string>;
};

function headersFromInit(init?: RequestInit): Record<string, string> {
  const out: Record<string, string> = {};
  if (!init?.headers) return out;
  new Headers(init.headers).forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

function mockFetchOnce(
  body: unknown = { status: 'ok' },
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = JSON.stringify(body);
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: input instanceof Request ? input.url : String(input),
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    // A fresh Response per call: a body can only be consumed once.
    return new Response(text, {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch, calls };
}

const observedHeaders = (call: FetchCall): Record<string, string> => ({
  ...call.initHeaders,
  ...call.inputHeaders,
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - static header key normalization (client.ts:173)', () => {
  it('accepts a non-empty string value and stores it under the lowercased key', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Tenant-ID': 'tenant-42' },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['x-tenant-id']).toBe('tenant-42');
    expect(headers['X-Tenant-ID']).toBeUndefined();
  });

  it('normalizes every caller key regardless of the casing they used', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'XCASE-Mixed': 'a',
        'xcase-lower': 'b',
        'XCase-UPPER': 'c',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['xcase-mixed']).toBe('a');
    expect(headers['xcase-lower']).toBe('b');
    expect(headers['xcase-upper']).toBe('c');
  });

  it('rejects the empty-string value and does not emit the header', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Empty': '', 'X-Keep': 'present' },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['x-empty']).toBeUndefined();
    expect(headers['x-keep']).toBe('present');
  });

  it('rejects non-string values even when the key is valid', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'X-Number': 7 as unknown as string,
        'X-Bool': true as unknown as string,
        'X-Null': null as unknown as string,
        'X-Object': { nested: 'v' } as unknown as string,
        'X-String': 'kept',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['x-number']).toBeUndefined();
    expect(headers['x-bool']).toBeUndefined();
    expect(headers['x-null']).toBeUndefined();
    expect(headers['x-object']).toBeUndefined();
    expect(headers['x-string']).toBe('kept');
  });

  it('emits a whitespace-only value, trimmed to empty by the Headers implementation', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Spaces': '   ' },
      fetch,
    });

    await sdk.getHealth();

    // The branch under test rejects only *zero-length* strings, so a
    // whitespace-only value is passed through; the WHATWG Headers
    // implementation then trims it. Pinning this keeps the boundary explicit.
    const headers = observedHeaders(calls[0]!);
    expect(Object.prototype.hasOwnProperty.call(headers, 'x-spaces')).toBe(true);
    expect(headers['x-spaces']).toBe('');
  });

  it('never lets a caller-supplied Authorization header through, at any casing', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        Authorization: 'Bearer attacker',
        authorization: 'Bearer attacker-lower',
        AUTHORIZATION: 'Bearer attacker-upper',
        'X-Auth-Proxy': 'Bearer proxy',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
    expect(headers['x-auth-proxy']).toBe('Bearer proxy');
  });

  it('always emits the SDK user-agent, overriding a caller-supplied one', async () => {
    const { fetch, calls } = mockFetchOnce();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'User-Agent': 'caller-agent/1.0' },
      fetch,
    });

    await sdk.getHealth();

    const headers = observedHeaders(calls[0]!);
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(headers['user-agent']).not.toBe('caller-agent/1.0');
  });

  it('applies the same normalization on every typed wrapper call', async () => {
    const { fetch, calls } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Trace': 'trace-1' },
      fetch,
    });

    await sdk.getHealth();
    await sdk.listPlans();
    await sdk.listSubscriptions();

    expect(calls).toHaveLength(3);
    for (const call of calls) {
      expect(observedHeaders(call)['x-trace']).toBe('trace-1');
    }
  });

  it('does not mutate the caller-supplied headers object', async () => {
    const { fetch } = mockFetchOnce();
    const supplied = { 'X-Mutate': 'original' };
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: supplied,
      fetch,
    });

    await sdk.getHealth();

    expect(supplied).toEqual({ 'X-Mutate': 'original' });
  });
});
