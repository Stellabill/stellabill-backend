import { describe, it, expect, beforeEach, afterEach, vitest } from 'vitest';

import {
  createStellarBillClient,
  normalizeErrorBody,
  isLocalhost,
  assertOk,
  type FetchLike,
  type StellarBillClientOptions,
  type SdkResult,
} from './client.js';
import { StellarBillConfigError, StellarBillError, type ApiErrorBody } from './errors.js';

// ----- Test helpers -----

/** Build a fake fetch that returns a canned Response. */
function makeFetch(response: Response): FetchLike {
  return async () => response.clone();
}

/** Build a fake fetch that records the outgoing request and returns a Response. */
function recordingFetch(response: Response): { fetch: FetchLike; calls: Request[] } {
  const calls: Request[] = [];
  const fetch: FetchLike = async (input, init) => {
    calls.push(new Request(input as RequestInfo, init));
    return response.clone();
  };
  return { fetch, calls };
}

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });
}

const BASE = 'https://api.stellabill.com';

function client(opts: Partial<StellarBillClientOptions> = {}) {
  return createStellarBillClient({ baseUrl: BASE, options });
}

// ----- Configuration validation -----

describe('createStellarBillClient configuration', () => {
  it('rejects a missing baseUrl', () => {
    expect(() => createStellarBillClient({ baseUrl: undefined as unknown as string })).toThrow(StellarBillConfigError);
  });

  it('rejects an empty baseUrl', () => {
    expect(() => createStellarBillClient({ baseUrl: '   ' })).toThrow(StellarBillConfigError);
  });

  it('rejects a malformed baseUrl', () => {
    expect(() => createStellarBillClient({ baseUrl: 'not a url' })).toThrow(StellarBillConfigError);
  });

  it('rejects when no fetch implementation is available', () => {
    const original = globalThis.fetch;
    // @ts-expect-error -- deliberately unset global fetch for this test.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = undefined;
    try {
      expect(() => createStellarBillClient({ baseUrl: BASE })).toThrow(StellarBillConfigError);
    } finally {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (globalThis as any).fetch = original;
    }
  });
});

// ----- Token handling -----

describe('token handling', () => {
  it('returns the sanitized token from getToken', () => {
    const sdk = client({ token: '  secret-token  ', fetch: makeFetch(jsonResponse(200, { status: 'ok' })) });
    expect(sdk.getToken()).toBe('secret-token');
  });

  it('rotates the token via setToken', () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, { status: 'ok' })) });
    expect(sdk.getToken()).toBeUndefined();
    sdk.newToken('new-token');
    expect(sdk.getToken()).toBe('new-token');
    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
  });

  it('injects the bearer token and user-agent on the outgoing request', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { status: 'ok' }));
    const sdk = client({ token: 'abc123', fetch });
    await sdk.getHealth();
    expect(calls.length).toBe(1);
    expect(calls[0].headers.get('authorization')).toBe('Bearer abc123');
    expect(calls[0].headers.get('user-agent')).toBeTruthy();
  });

  it('does not inject authorization when no token is configured', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { status: 'ok' }));
    const sdk = client({ fetch });
    await sdk.getHealth();
    expect(calls[0].headers.has('authorization')).toBe(false);
  });

  it('rejects caller-supplied Authorization headers', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { status: 'ok' }));
    const sdk = client({
      token: 'real-token',
      fetch,
      headers: { Authorization: 'Bearer smuggled', 'X-Tenant-ID': 'tenant-1' },
    });
    await sdk.getHealth();
    expect(calls[0].headers.get('authorization')).toBe('Bearer real-token');
    expect(calls[0].headers.get('x-tenant-id')).toBe('tenant-1');
  });
});

// ----- Success paths -----

describe('success paths', () => {
  it('returns the parsed body and metadata for getHealth', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, { status: 'ok' })) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(200);
    expect(result.data).toEqual({ status: 'ok' });
    expect(result.error).toBeUndefined();
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
  });

  it('forwards listPlans query parameters', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { plans: [] }));
    const sdk = client({ fetch });
    await sdk.listPlans({ cursor: 'abc', limit: 10 });
    const url = new URL(calls[0].url);
    expect(url.pathname).toBe('/api/v1/plans');
    expect(url.searchParams.get('cursor')).toBe('abc');
    expect(url.searchParams.get('limit')).toBe('10');
  });

  it('forwards listSubscriptions query parameters', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { subscriptions: [] }));
    const sdk = client({ fetch });
    await sdk.listSubscriptions({ cursor: 'xyz', limit: 5 });
    const url = new URL(calls[0].url);
    expect(url.pathname).toBe('/api/subscriptions');
    expect(url.searchParams.get('cursor')).toBe('xyz');
    expect(url.searchParams.get('limit')).toBe('5');
  });

  it('encodes the subscription id in the path', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { id: 'a' }));
    const sdk = client({ fetch });
    await sdk.getSubscription('a/b c');
    const url = new URL(calls[0].url);
    expect(url.pathname).toBe('/api/subscriptions/a%2Fb%20c');
  });

  it('encodes the idlempotency key in the path', async () => {
    const { fetch, calls } = recordingFetch(jsonResponse(200, { key: 'k' }));
    const sdk = client({ fetch });
    await sdk.inspectIdlempotencyKey('key/123');
    const url = new URL(calls[0].url);
    expect(url.pathname).toBe('/api/v1/idlempotency/key%2F123');
  });
});

// ----- Rejected input paths (contract coverage) -----

describe('rejected input paths', () => {
  it('rejects an empty subscription id with a config error', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, {})) });
    await expect(sdk.getSubscription('')).rejects.toThrow(StellarBillConfigError);
  });

  it('rejects a non-string subscription id with a config error', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, {})) });
    await expect(sdk.getSubscription(123 as unknown as string)).rejects.toThrow(StellarBillConfigError);
  });

  it('rejects an empty idlempotency key with a config error', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, {})) });
    await expect(sdk.inspectIdlempotencyKey('')).rejects.toThrow(StellarBillConfigError);
  });

  it('rejects an idlempotency key longer than 255 characters', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, {})) });
    await expect(sdk.inspectIdlempotencyKey('a'.repeat(256))).rejects.toThrow(StellarBillConfigError);
  });

  it('accepts an idlempotency key of exactly 255 characters', async () => {
    const sdk = client({ fetch: makeFetch(jsonResponse(200, { key: 'k' })) });
    const result = await sdk.inspectIdlempotencyKey('a'.repeat(255));
    expect(result.status).toBe(200);
  });
});

// ----- Error body normalization (empty text rejection path) -----

describe('error response handling', () => {
  it('returns undefined error for an empty JSON body (empty-text rejection path)', async () => {
    // content-type is JSON but the body is empty: the `safeParseErrorBody`
    // `if (!text) return undefined;` branch must yield a stable undefined error.
    const resp = new Response(null, {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
    expect(result.data).toBeUndefined();
  });

  it('returns undefined error for a whitespace-only JSON body', async () => {
    // Whitespace is non-empty text, but JSON.parse throws, so the catch
    // branch returns undefined. Asserts the deterministic contract.
    const resp = new Response('   ', {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error for malformed JSON', async () => {
    const resp = new Response('{ not json', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(400);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error for a non-JSON content type', async () => {
    const resp = new Response('not json', {
      status: 503,
      headers: { 'content-type': 'text/plain' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(503);
    expect(result.error).toBeUndefined();
  });

  it('surfaces a parsed JSON error body', async () => {
    const sdk = client({
      fetch: makeFetch(jsonResponse(404, { message: 'not found' })),
    });
    const result = await sdk.getHealth();
    expect(result.status).toBe(404);
    expect(result.error).toEqual({ message: 'not found' });
  });

  it('returns undefined error for a JSON array body (normalizeErrorBody rejects arrays)', async () => {
    const resp = new Response(JSON.stringify([1, 2, 3]), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(400);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error for a JSON null body', async () => {
    const resp = new Response('null', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ fetch: makeFetch(resp) });
    const result = await sdk.getHealth();
    expect(result.status).toBe(400);
    expect(result.error).toBeUndefined();
  });
});

// ----- throwOnError -----

describe('throwOnError', () => {
  it('throws a StellarBillError with an actionable message on non-2xx', async () => {
    const sdk = client({
      throwOnError: true,
      fetch: makeFetch(jsonResponse(422, { message: 'invalid payload' })),
    });
    await expect(sdk.getHealth()).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(StellarBillError);
      const e = err as StellarBillError;
      expect(e.status).toBe(422);
      expect(e.message).toContain('422');
      expect(e.message).toContain('invalid payload');
      return true;
    });
  });

  it('throws with an HTTP status fallback when the error body is empty', async () => {
    // Empty JSON body -> safeParseErrorBody returns undefined -> message
    // falls back to `HTTP <status>`. This asserts the rejection path is
    // observable and deterministic.
    const resp = new Response(null, {
      status: 500,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = client({ throwOnError: true, fetch: makeFetch(resp) });
    await expect(sdk.getHealth()).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(StellarBillError);
      const e = err as StellarBillError;
      expect(e.status).toBe(500);
      expect(e.message).toContain('HTTP 500');
      return true;
    });
  });
});
