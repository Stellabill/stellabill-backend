import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Accepted-input coverage for the caller-header normalization loop in
 * `sdks/ts/src/client.ts:172`:
 *
 *   const lower = k.toLowerCase();
 *   if (lower === 'authorization') continue;
 *
 * The issue asks for a representative valid input to be accepted by this
 * branch and for the documented result/state to be preserved. These tests
 * pin the positive path: a realistic caller header bag containing an
 * `authorization` key is accepted without throwing, the documented
 * normalization (keys lower-cased, values verbatim, empty values skipped)
 * holds, and the caller object is not mutated.
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

afterEach(() => {
  vi.restoreAllMocks();
});

describe('caller header normalization - accepted input (client.ts:172)', () => {
  it('accepts a representative header bag containing authorization and preserves the documented result', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const headers = {
      authorization: 'Bearer caller-token',
      'X-Tenant-Id': 'tenant-1',
      'X-Trace-Id': 'trace-abc',
    };

    const sdk = createStellarBillClient({ baseUrl: BASE, headers, fetch });
    const result = await sdk.getHealth();

    // The documented result/state of the request is preserved: 200 + parsed data.
    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data?.status).toBe('ok');

    const sent = callHeaders(calls[0]!);
    // Accepted non-auth headers are forwarded with lower-cased names, values verbatim.
    expect(sent['x-tenant-id']).toBe('tenant-1');
    expect(sent['x-trace-id']).toBe('trace-abc');
    // The accepted authorization input is dropped deterministically (no token configured).
    expect(sent['authorization']).toBeUndefined();
  });

  it('does not mutate the caller-provided headers object (state preserved)', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const headers: Record<string, string> = {
      Authorization: 'Bearer caller-token',
      'X-Tenant-Id': 'tenant-1',
    };
    const snapshot = { ...headers };

    const sdk = createStellarBillClient({ baseUrl: BASE, headers, fetch });
    await sdk.getHealth();

    expect(headers).toEqual(snapshot);
  });

  it('accepts a mixed-case authorization key and lets the configured SDK token win', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'sdk-token',
      headers: { AuThOrIzAtIoN: 'Bearer caller-token', 'X-Tenant-Id': 'tenant-1' },
      fetch,
    });

    const result = await sdk.getHealth();
    expect(result.status).toBe(200);

    const sent = callHeaders(calls[0]!);
    expect(sent['authorization']).toBe('Bearer sdk-token');
    expect(sent['x-tenant-id']).toBe('tenant-1');
  });

  it('preserves the accepted state deterministically across repeated requests', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      headers: { authorization: 'Bearer caller-token', 'X-Tenant-Id': 'tenant-1' },
      fetch,
    });

    await sdk.getHealth();
    await sdk.getHealth();

    expect(calls).toHaveLength(2);
    const first = callHeaders(calls[0]!);
    const second = callHeaders(calls[1]!);
    expect(first['x-tenant-id']).toBe('tenant-1');
    expect(second['x-tenant-id']).toBe('tenant-1');
    expect(first['authorization']).toBeUndefined();
    expect(second['authorization']).toBeUndefined();
  });

  it('accepts an empty-string authorization value without throwing (documented drop)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      headers: { authorization: '', 'X-Tenant-Id': 'tenant-1' },
      fetch,
    });

    const result = await sdk.getHealth();
    expect(result.status).toBe(200);

    const sent = callHeaders(calls[0]!);
    expect(sent['authorization']).toBeUndefined();
    expect(sent['x-tenant-id']).toBe('tenant-1');
  });
});
