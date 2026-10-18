import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, sanitizeToken, TokenHolder } from '../src/index.js';

type FetchCall = {
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

function mockFetch(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const res = new Response(JSON.stringify({ status: 'ok' }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return res;
  });
  return { fetch, calls };
}

const observedAuthorization = (call: FetchCall): string | undefined => ({
  ...call.initHeaders,
  ...call.inputHeaders,
})['authorization'];

afterEach(() => {
  vi.restoreAllMocks();
});

describe('sanitizeToken rejection contract', () => {
  it('rejects every malformed token shape with a stable undefined', () => {
    expect(sanitizeToken('')).toBeUndefined();
    expect(sanitizeToken('   ')).toBeUndefined();
    expect(sanitizeToken('\t')).toBeUndefined();
    expect(sanitizeToken('\n')).toBeUndefined();
    expect(sanitizeToken('bad token')).toBeUndefined();
    expect(sanitizeToken('a b')).toBeUndefined();
    expect(sanitizeToken(42 as unknown as string)).toBeUndefined();
    expect(sanitizeToken(null as unknown as string)).toBeUndefined();
    expect(sanitizeToken(undefined)).toBeUndefined();
  });

  it('accepts a well-formed token and trims surrounding whitespace', () => {
    expect(sanitizeToken('  abc.def-ghi_123  ')).toBe('abc.def-ghi_123');
  });
});

describe('TokenHolder rejection contract', () => {
  it('reports no token for any rejected value', () => {
    const holder = new TokenHolder(sanitizeToken('bad token'));
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('clears a previously accepted token when a rejected value is set', () => {
    const holder = new TokenHolder(sanitizeToken('good-token'));
    expect(holder.hasToken()).toBe(true);

    holder.set(sanitizeToken('bad token'));
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });
});

describe('createStellarBillClient - rejected token input (client.ts:30)', () => {
  it('returns undefined from getToken when the constructor token is rejected', () => {
    const { fetch } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });

    expect(sdk.getToken()).toBeUndefined();
  });

  it('emits no Authorization header when the constructor token is rejected', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });

    await sdk.getHealth();

    expect(observedAuthorization(calls[0]!)).toBeUndefined();
  });

  it('treats whitespace-only and empty constructor tokens as rejected', () => {
    const { fetch } = mockFetch();
    for (const token of ['', '   ', '\n']) {
      const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token, fetch });
      expect(sdk.getToken()).toBeUndefined();
    }
  });

  it('drops the previously accepted token when setToken receives rejected input', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'good-token',
      fetch,
    });
    expect(sdk.getToken()).toBe('good-token');

    sdk.setToken('bad token');

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(observedAuthorization(calls[0]!)).toBeUndefined();
  });

  it('keeps getToken() returning undefined across repeated rejected reads', () => {
    const { fetch } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'has space',
      fetch,
    });

    expect(sdk.getToken()).toBeUndefined();
    expect(sdk.getToken()).toBeUndefined();
    expect(sdk.getToken()).toBeUndefined();
  });

  it('recovers once a valid token is supplied after rejection', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });

    sdk.setToken('  fresh-token  ');

    expect(sdk.getToken()).toBe('fresh-token');
    await sdk.getHealth();
    expect(observedAuthorization(calls[0]!)).toBe('Bearer fresh-token');
  });

  it('rejects non-string token input passed to setToken', () => {
    const { fetch } = mockFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'good-token',
      fetch,
    });

    sdk.setToken(123 as unknown as string);
    expect(sdk.getToken()).toBeUndefined();
  });
});
