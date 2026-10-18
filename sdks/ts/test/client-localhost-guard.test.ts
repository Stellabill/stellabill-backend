import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Focused coverage for the `isLocalhost()` guard reached from
 * `createStellarBillClient`'s insecure-baseUrl warning
 * (`sdks/ts/src/client.ts`).
 *
 * The guard is:
 *
 *   const u = new URL(baseUrl);
 *   return u.hostname === 'localhost' || u.hostname === '127.0.0.1';
 *
 * and it is only observable through the `console.warn` emitted for plain
 * `http://` base URLs. The existing suite already covers the happy cases
 * (`http://example.com` warns, `https://…` does not, `localhost`/`127.0.0.1`
 * with a port do not). These tests target the rejection/boundary gaps:
 * hostname *suffix look-alikes*, case-folding, the port-less loopback form,
 * IPv6 loopback spellings, and the `https` look-alike.
 *
 * NOTE ON THE IPv6 CASES: `URL#hostname` keeps the square brackets, so
 * `new URL('http://[::1]:9000').hostname === '[::1]'`. Because the guard
 * compares against the bare literals `'localhost'` and `'127.0.0.1'`, IPv6
 * loopback is treated as *non*-localhost today and therefore warns. These
 * assertions pin the current behavior as a documented regression guard — they
 * deliberately do NOT change production behavior.
 */

type FetchCall = {
  url: string;
  inputHeaders: Record<string, string>;
  initHeaders: Record<string, string>;
};

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

function mockFetchOnce(
  body: unknown,
  status = 200,
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, {
    status,
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
      url: toUrl(input),
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return res;
  });
  return { fetch, calls };
}

function callHeaders(call: FetchCall): Record<string, string> {
  return { ...call.initHeaders, ...call.inputHeaders };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - localhost guard boundaries', () => {
  it('warns for hostname suffix look-alikes that are not loopback', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const { fetch: f1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    createStellarBillClient({ baseUrl: 'http://localhost.evil.com', fetch: f1 });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('http://localhost.evil.com'));

    warn.mockClear();

    const { fetch: f2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    createStellarBillClient({ baseUrl: 'http://127.0.0.1.evil.com', fetch: f2 });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('http://127.0.0.1.evil.com'));
  });

  it('does not warn when the hostname is upper-cased loopback (URL lowercases it)', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });

    const sdk = createStellarBillClient({ baseUrl: 'http://LOCALHOST:3000', fetch });
    await sdk.getHealth();

    expect(warn).not.toHaveBeenCalled();
    expect(calls[0]!.url).toBe('http://localhost:3000/api/health');
  });

  it('does not warn for the port-less localhost form', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });

    const sdk = createStellarBillClient({ baseUrl: 'http://localhost', fetch });
    await sdk.getHealth();

    expect(warn).not.toHaveBeenCalled();
    expect(calls[0]!.url).toBe('http://localhost/api/health');
  });

  it('warns for https look-alikes only when the scheme is insecure', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });

    // Same deceptive hostname as the http case above, but https => no warning.
    createStellarBillClient({ baseUrl: 'https://localhost.evil.com', fetch });

    expect(warn).not.toHaveBeenCalled();
  });

  it('documents that IPv6 loopback is currently treated as non-localhost (regression guard)', async () => {
    // `URL#hostname` retains the brackets for IPv6 hosts ('[::1]'), so the
    // guard's bare-string comparison misses them and the insecure-baseUrl
    // warning fires. Pinned here so a future fix that normalizes IPv6
    // loopback is an explicit, test-visible decision rather than a silent
    // behavior change.
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const { fetch: f1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    createStellarBillClient({ baseUrl: 'http://[::1]:9000', fetch: f1 });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('http://[::1]:9000'));

    warn.mockClear();

    const { fetch: f2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    // The expanded IPv6 spelling is canonicalized by `validateBaseUrl`
    // (URL#toString) before the warning is emitted, so the message shows
    // the compressed `[::1]` form even though the caller passed the long
    // form. Both spellings take the same non-localhost branch.
    createStellarBillClient({ baseUrl: 'http://[0:0:0:0:0:0:0:1]:9000', fetch: f2 });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('http://[::1]:9000'));
  });

  it('still sends an authenticated request through when an insecure non-localhost baseUrl warns', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });

    const sdk = createStellarBillClient({
      baseUrl: 'http://localhost.evil.com',
      token: '  my-token  ',
      fetch,
    });
    const r = await sdk.getHealth();

    expect(warn).toHaveBeenCalledTimes(1);
    expect(r.status).toBe(200);
    expect(r.data?.status).toBe('ok');
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe('http://localhost.evil.com/api/health');
    expect(callHeaders(calls[0]!)['authorization']).toBe('Bearer my-token');
  });
});
