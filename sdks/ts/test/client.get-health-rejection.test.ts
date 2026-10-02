import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

/**
 * Regression coverage for the rejected-input contract of the `sdk` client at
 * the documented `sdk.getHealth()` call site (sdks/ts/src/client.ts:141).
 *
 * The SDK must never turn a rejected request into a resolved envelope: a
 * transport rejection has to surface verbatim, and a server-side rejection
 * has to be observable either as a thrown `StellarBillError` (throwOnError) or
 * as a stable `error` body on the result. These cases pin the request shape
 * (GET /api/health) and the rejection envelope so the contract cannot drift.
 */

type Captured = { url: string; method: string; headers: Record<string, string> };

function rejectingFetch(error: unknown): { fetch: typeof globalThis.fetch; calls: Captured[] } {
  const calls: Captured[] = [];
  const fetch = (async (input: RequestInfo, init?: RequestInit) => {
    const url = input instanceof Request ? input.url : String(input);
    const method = (input instanceof Request ? input.method : init?.method) ?? 'GET';
    const headers: Record<string, string> = {};
    if (input instanceof Request) input.headers.forEach((v, k) => (headers[k.toLowerCase()] = v));
    calls.push({ url, method, headers });
    throw error;
  }) as unknown as typeof globalThis.fetch;
  return { fetch, calls };
}

function jsonFetch(
  body: unknown,
  status: number,
): { fetch: typeof globalThis.fetch; calls: Captured[] } {
  const calls: Captured[] = [];
  const fetch = (async (input: RequestInfo, init?: RequestInit) => {
    const url = input instanceof Request ? input.url : String(input);
    const method = (input instanceof Request ? input.method : init?.method) ?? 'GET';
    calls.push({ url, method, headers: {} });
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    });
  }) as unknown as typeof globalThis.fetch;
  return { fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('sdk.getHealth() - rejected input (client.ts:141)', () => {
  it('rejects with the original transport error instead of resolving an envelope', async () => {
    const networkError = new TypeError('fetch failed: ENOTFOUND api.example.com');
    const { fetch } = rejectingFetch(networkError);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await expect(sdk.getHealth()).rejects.toBe(networkError);
  });

  it('propagates an abort rejection verbatim', async () => {
    const abort = new DOMException('The operation was aborted.', 'AbortError');
    const { fetch } = rejectingFetch(abort);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await expect(sdk.getHealth()).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('sends GET /api/health and attaches the bearer token on the rejected request', async () => {
    const { fetch, calls } = rejectingFetch(new Error('boom'));
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok-123',
      fetch,
    });

    await expect(sdk.getHealth()).rejects.toThrow('boom');
    expect(calls).toHaveLength(1);
    expect(calls[0]!.method).toBe('GET');
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
    expect(calls[0]!.headers['authorization']).toBe('Bearer tok-123');
  });

  it('throws StellarBillError (with stable contract) when the server rejects and throwOnError is set', async () => {
    const { fetch } = jsonFetch(
      { error: 'Unauthorized', message: 'token expired', code: 'auth_token_expired' },
      401,
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    let caught: unknown;
    try {
      await sdk.getHealth();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillError);
    const e = caught as StellarBillError;
    expect(e.name).toBe('StellarBillError');
    expect(e.status).toBe(401);
    expect(e.requestMethod).toBe('GET');
    expect(e.requestUrl).toContain('/api/health');
    expect(e.body).toEqual({
      error: 'Unauthorized',
      message: 'token expired',
      code: 'auth_token_expired',
    });
    expect(e.message).toBe('GET /api/health failed (401): token expired');
    expect(e.toString()).toContain('auth_token_expired');
  });

  it('resolves with a rejected envelope (no throw) when throwOnError is not set', async () => {
    const { fetch } = jsonFetch(
      { error: 'Service Unavailable', message: 'downstream down', code: 'svc_unavailable' },
      503,
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();
    expect(r.status).toBe(503);
    expect(r.data).toBeUndefined();
    expect(r.error?.code).toBe('svc_unavailable');
    expect(r.requestMethod).toBe('GET');
  });
});
