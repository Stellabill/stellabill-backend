import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

/**
 * Accepted-input / boundary coverage for the `parsedError` gate in
 * `sdks/ts/src/client.ts:228`:
 *
 *   if (throwOnError && (status < 200 || status >= 300)) {
 *
 * The issue asks that a representative valid input crossing this branch is
 * accepted and that the documented result is preserved. This suite pins the
 * accept band `[200, 300)`: 200 and 299 (and intermediate 2xx codes) must not
 * throw, 300 must, and a 2xx body that merely *looks* like an error envelope
 * must still be delivered as `data` because the gate keys on status, not body
 * shape.
 */

const BASE = 'https://api.example.com';

type FetchCall = { url: string };

function mockFetchOnce(
  body: unknown,
  init: { status?: number } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  // 204 must not carry a body; otherwise a fresh Response is built per call so
  // repeated requests in a single test each get their own readable body.
  const makeRes = (): Response =>
    status === 204
      ? new Response(null, { status })
      : new Response(text, { status, headers: { 'content-type': 'application/json' } });
  const fetch: typeof globalThis.fetch = vi.fn(async (input) => {
    calls.push({ url: typeof input === 'string' ? input : input instanceof Request ? input.url : String(input) });
    return makeRes();
  });
  return { fetch, calls };
}

/** A synthetic non-standard response (status < 200 cannot be built with `new Response`). */
function synthResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    url: `${BASE}/api/health`,
    headers: new Headers({ 'content-type': 'application/json', 'content-length': '1' }),
    text: async () => JSON.stringify(body),
    json: async () => body,
  } as unknown as Response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('throwOnError accepted-input boundary (client.ts:228)', () => {
  it.each([200, 201, 202, 206, 250, 299])(
    'accepts 2xx status %i without throwing and preserves the result envelope',
    async (status) => {
      const payload = { status: 'ok', service: 'stellarbill-backend' };
      const { fetch } = mockFetchOnce(payload, { status });
      const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

      const result = await sdk.getHealth();

      expect(result.status).toBe(status);
      expect(result.error).toBeUndefined();
      expect(result.data).toEqual(payload);
      expect(result.requestMethod).toBe('GET');
      expect(result.requestUrl).toContain('/api/health');
    },
  );

  it('accepts 204 No Content without throwing when throwOnError is true', async () => {
    const { fetch } = mockFetchOnce(null, { status: 204 });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(204);
    expect(result.error).toBeUndefined();
    expect(result.data).toBeUndefined();
  });

  it('accepts a 2xx body that looks like an error envelope (the gate keys on status, not body shape)', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Nope', message: 'looks bad', code: 'weird' },
      { status: 200 },
    );
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data).toEqual({ error: 'Nope', message: 'looks bad', code: 'weird' });
  });

  it('rejects status 300 (upper boundary) when throwOnError is true', async () => {
    const { fetch } = mockFetchOnce({ error: 'Multiple Choices', message: 'moved', code: 'x' }, { status: 300 });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    await expect(sdk.getHealth()).rejects.toBeInstanceOf(StellarBillError);
    await expect(sdk.getHealth()).rejects.toMatchObject({ status: 300 });
  });

  it('accepts status 300 (no throw) when throwOnError is omitted, exposing the parsed error envelope', async () => {
    const { fetch } = mockFetchOnce({ error: 'Multiple Choices', message: 'moved', code: 'x' }, { status: 300 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(300);
    expect(result.data).toBeUndefined();
    expect(result.error?.code).toBe('x');
  });

  it('rejects status 199 (lower boundary) when throwOnError is true', async () => {
    const fetch = vi.fn(async () => synthResponse(199, { error: 'Early Hints', message: 'in limbo', code: 'x' }));
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch: fetch as unknown as typeof globalThis.fetch,
    });

    await expect(sdk.getHealth()).rejects.toBeInstanceOf(StellarBillError);
    await expect(sdk.getHealth()).rejects.toMatchObject({ status: 199 });
  });

  it('accepts status 199 (no throw) when throwOnError is omitted, exposing the parsed error envelope', async () => {
    const fetch = vi.fn(async () => synthResponse(199, { error: 'Early Hints', message: 'in limbo', code: 'x' }));
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      fetch: fetch as unknown as typeof globalThis.fetch,
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(199);
    expect(result.data).toBeUndefined();
    expect(result.error?.code).toBe('x');
  });
});
