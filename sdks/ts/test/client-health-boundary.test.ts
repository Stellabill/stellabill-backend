/**
 * Boundary coverage for the `sdk.getHealth()` wrapper (client.ts:141 / :252).
 *
 * `wrap()` treats any status in [200, 300) as accepted and everything else as
 * an error; `getHealth` is the thinnest public surface over that rule. These
 * cases pin the exact 2xx boundary, the `throwOnError` fork, and the
 * `response.url || urlPath` fallback in the result envelope.
 */
import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

type FetchCall = { url: string; init: RequestInit | undefined };

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

function mockFetchOnce(
  body: unknown,
  init: { status?: number; contentType?: string; url?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  let text = '';
  if (body !== undefined) text = typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, { status, headers: { 'content-type': contentType } });
  if (init.url !== undefined) {
    Object.defineProperty(res, 'url', { value: init.url, configurable: true });
  }
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    calls.push({ url: toUrl(input), init: initArg as RequestInit | undefined });
    return res;
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';
const HEALTH_OK = { status: 'ok', service: 'stellarbill-backend' };

describe('getHealth — accepted 2xx boundary', () => {
  it('returns the parsed payload and envelope for 200', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data?.status).toBe('ok');
    expect(r.requestMethod).toBe('GET');
  });

  it('accepts 201 (low end of the accepted band) with throwOnError:true', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, { status: 201 });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    await expect(sdk.getHealth()).resolves.toMatchObject({ status: 201 });
  });

  it('accepts 299 (high end of the accepted band) with throwOnError:true', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, { status: 299 });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    await expect(sdk.getHealth()).resolves.toMatchObject({ status: 299 });
  });

  it('rejects 300 (first non-2xx status) with throwOnError:true', async () => {
    // NOTE: a Response body can only be consumed once, so call `getHealth`
    // exactly once and inspect the thrown error.
    const { fetch } = mockFetchOnce({ message: 'redirect' }, { status: 300 });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    let caught: unknown;
    try {
      await sdk.getHealth();
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(StellarBillError);
    expect((caught as StellarBillError).status).toBe(300);
    expect((caught as StellarBillError).requestMethod).toBe('GET');
  });
});

describe('getHealth — rejected 2xx boundary', () => {
  it('does not throw on 300 when throwOnError is false', async () => {
    const { fetch } = mockFetchOnce({ message: 'redirect' }, { status: 300 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(300);
    expect(r.data).toBeUndefined();
    expect(r.error?.message).toBe('redirect');
  });

  it('surfaces a non-JSON error body as undefined without throwing', async () => {
    const { fetch } = mockFetchOnce('<html>nope</html>', {
      status: 500,
      contentType: 'text/html',
    });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(500);
    expect(r.error).toBeUndefined();
  });
});

describe('getHealth — requestUrl envelope', () => {
  it('falls back to the request path when response.url is empty', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.requestUrl).toBe('/api/health');
  });

  it('uses response.url when the transport populates it', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, {
      status: 200,
      url: 'https://api.example.com/api/health?trace=1',
    });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.requestUrl).toBe('https://api.example.com/api/health?trace=1');
  });

  it('exposes the original Response on the envelope', async () => {
    const { fetch } = mockFetchOnce(HEALTH_OK, { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.response).toBeInstanceOf(Response);
    expect(r.response.status).toBe(200);
    expect(r.response.headers.get('content-type')).toContain('application/json');
  });
});
