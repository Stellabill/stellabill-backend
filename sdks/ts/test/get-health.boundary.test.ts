import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError, type FetchLike } from '../src/index.js';

/**
 * Boundary coverage for `getHealth` (`sdks/ts/src/client.ts:251-254`).
 *
 * `getHealth` is the SDK's smoke-test call, so it is the one wrapper that runs
 * with no parameters at all. Its two lines decide:
 *
 *  - the exact request path (`/api/health`) that the raw `openapi-fetch`
 *    client is asked for, and
 *  - the envelope `wrap()` returns for whatever `res` it gets back.
 *
 * These tests pin both, across the 2xx/3xx boundary, an empty 2xx body, and the
 * `throwOnError` shortcut.
 */

const BASE = 'https://api.stellabill.com';

type Call = { url: string; method: string };

function recordingFetch(
  make: () => Response,
): { fetch: FetchLike; calls: Call[] } {
  const calls: Call[] = [];
  const fetch = (async (input: RequestInfo, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    calls.push({ url, method });
    return make();
  }) as unknown as FetchLike;
  return { fetch, calls };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

describe('getHealth - request shape', () => {
  it('issues a parameterless GET to /api/health', async () => {
    const { fetch, calls } = recordingFetch(() => jsonResponse({ status: 'ok' }));
    const client = createStellarBillClient({ baseUrl: BASE, fetch });

    await client.getHealth();

    expect(calls).toHaveLength(1);
    expect(calls[0].method).toBe('GET');
    expect(calls[0].url).toBe(`${BASE}/api/health`);
  });

  it('normalises a baseUrl with trailing slashes before composing the path', async () => {
    const { fetch, calls } = recordingFetch(() => jsonResponse({ status: 'ok' }));
    const client = createStellarBillClient({ baseUrl: `${BASE}///`, fetch });

    await client.getHealth();

    expect(calls[0].url).toBe(`${BASE}/api/health`);
  });

  it('composes the health path relative to a baseUrl that already has a path prefix', async () => {
    const { fetch, calls } = recordingFetch(() => jsonResponse({ status: 'ok' }));
    const client = createStellarBillClient({ baseUrl: `${BASE}/gateway`, fetch });

    await client.getHealth();

    expect(calls[0].url).toBe(`${BASE}/gateway/api/health`);
  });

  it('does not send an Authorization header when no token was configured', async () => {
    let seen: Headers | undefined;
    const fetch = (async (input: RequestInfo) => {
      seen = input instanceof Request ? input.headers : undefined;
      return jsonResponse({ status: 'ok' });
    }) as unknown as FetchLike;
    const client = createStellarBillClient({ baseUrl: BASE, fetch });

    await client.getHealth();

    expect(seen?.has('authorization')).toBe(false);
  });

  it('is repeatable without leaking state between calls', async () => {
    const { fetch, calls } = recordingFetch(() => jsonResponse({ status: 'ok' }));
    const client = createStellarBillClient({ baseUrl: BASE, fetch });

    const first = await client.getHealth();
    const second = await client.getHealth();

    expect(calls).toHaveLength(2);
    expect(first.status).toBe(200);
    expect(second.status).toBe(200);
  });
});

describe('getHealth - result envelope', () => {
  it('returns data, status, response, method and the fallback urlPath', async () => {
    const { fetch } = recordingFetch(() => jsonResponse({ status: 'ok', version: '1.2.3' }));
    const client = createStellarBillClient({ baseUrl: BASE, fetch });

    const result = await client.getHealth();

    expect(result.data).toEqual({ status: 'ok', version: '1.2.3' });
    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.response).toBeInstanceOf(Response);
    expect(result.requestMethod).toBe('GET');
    // A mock Response has an empty `url`, so the wrapper falls back to the path.
    expect(result.requestUrl).toBe('/api/health');
  });

  it('prefers the response URL over the fallback path when the transport provides one', async () => {
    const res = jsonResponse({ status: 'ok' });
    Object.defineProperty(res, 'url', { value: `${BASE}/api/health`, configurable: true });
    const client = createStellarBillClient({ baseUrl: BASE, fetch: (async () => res) as unknown as FetchLike });

    const result = await client.getHealth();

    expect(result.requestUrl).toBe(`${BASE}/api/health`);
  });

  it('reports a 2xx-with-empty-body response as success with undefined data', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      fetch: (async () => new Response(null, { status: 204 })) as unknown as FetchLike,
    });

    const result = await client.getHealth();

    expect(result.status).toBe(204);
    expect(result.data).toBeUndefined();
    expect(result.error).toBeUndefined();
  });
});

describe('getHealth - 2xx/3xx status boundary', () => {
  it('treats status 299 as a success', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      fetch: (async () => jsonResponse({ status: 'ok' }, 299)) as unknown as FetchLike,
    });

    const result = await client.getHealth();

    expect(result.status).toBe(299);
    expect(result.data).toEqual({ status: 'ok' });
  });

  it('treats status 300 as a failure but still returns an envelope by default', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      fetch: (async () => jsonResponse({ message: 'multiple choices' }, 300)) as unknown as FetchLike,
    });

    const result = await client.getHealth();

    expect(result.status).toBe(300);
    expect(result.data).toBeUndefined();
    expect(result.error).toEqual({ message: 'multiple choices' });
  });

  it('throws at status 300 when throwOnError is enabled', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch: (async () => jsonResponse({ message: 'multiple choices' }, 300)) as unknown as FetchLike,
    });

    await expect(client.getHealth()).rejects.toThrow(
      'GET /api/health failed (300): multiple choices',
    );
  });

  it('throws a StellarBillError carrying the status and method at the boundary', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch: (async () => jsonResponse({ error: 'boom' }, 500)) as unknown as FetchLike,
    });

    const rejection = await client.getHealth().catch((e) => e);

    expect(rejection).toBeInstanceOf(StellarBillError);
    expect((rejection as StellarBillError).status).toBe(500);
    expect((rejection as StellarBillError).requestMethod).toBe('GET');
    expect((rejection as StellarBillError).requestUrl).toBe('/api/health');
  });

  it('does not throw at status 299 when throwOnError is enabled', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch: (async () => jsonResponse({ status: 'ok' }, 299)) as unknown as FetchLike,
    });

    await expect(client.getHealth()).resolves.toMatchObject({ status: 299 });
  });

  it('leaves a transport rejection to the caller', async () => {
    const client = createStellarBillClient({
      baseUrl: BASE,
      fetch: (async () => {
        throw new TypeError('network down');
      }) as unknown as FetchLike,
    });

    await expect(client.getHealth()).rejects.toThrow('network down');
  });

  it('calls the injected fetch exactly once per getHealth call', async () => {
    const spy = vi.fn(async () => jsonResponse({ status: 'ok' }));
    const client = createStellarBillClient({ baseUrl: BASE, fetch: spy as unknown as FetchLike });

    await client.getHealth();

    expect(spy).toHaveBeenCalledTimes(1);
  });
});
