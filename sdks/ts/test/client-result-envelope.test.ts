/**
 * Accepted-input coverage for the `wrap()` result envelope (client.ts:222 —
 * `const r = (await rawResult) as { data, error, response }`).
 *
 * `wrap()` maps an `openapi-fetch` result onto `SdkResult`. This suite drives
 * the accepted path through the public wrappers and pins every field of the
 * envelope plus the `parsedError` normalisation rule (only object-shaped
 * errors are surfaced; non-object errors become `undefined`).
 */
import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

type FetchCall = { url: string; init: RequestInit | undefined };

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
    calls.push({ url: typeof input === 'string' ? input : String(input), init: initArg as RequestInit | undefined });
    return res;
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';

describe('wrap() — accepted input envelope', () => {
  it('maps a resolved 200 object result to a fully populated envelope', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(200);
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toBe('/api/health');
    expect(r.response).toBeInstanceOf(Response);
  });

  it('accepts an array payload without coercing it', async () => {
    const { fetch } = mockFetchOnce([{ id: 'p1' }]);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.listPlans();

    expect(r.data).toEqual([{ id: 'p1' }]);
    expect(r.status).toBe(200);
  });

  it('accepts an empty object payload (data is defined, not undefined)', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.data).toEqual({});
    expect(r.error).toBeUndefined();
  });

  it('does not throw for an accepted 2xx result when throwOnError is true', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok' });
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true, fetch });

    await expect(sdk.getHealth()).resolves.toMatchObject({ status: 200 });
  });
});

describe('wrap() — parsedError normalisation', () => {
  it('surfaces an object error body as-is', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400 },
    );
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.listPlans({ limit: 999 });

    expect(r.status).toBe(400);
    expect(r.error).toEqual({
      error: 'Bad Request',
      message: 'Invalid cursor',
      code: 'invalid_cursor',
    });
    expect(r.data).toBeUndefined();
  });

  it('normalises a non-object (string) error body to undefined', async () => {
    // openapi-fetch hands back the parsed JSON value; a JSON string body is a
    // valid response, so the SDK must not pretend it is an ApiErrorBody.
    const { fetch } = mockFetchOnce('"boom"', { status: 502 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(502);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();
  });

  it('surfaces an array error body as-is (arrays are objects)', async () => {
    // `typeof [] === 'object'`, so the parsedError guard keeps arrays rather
    // than discarding them. Pin that behaviour so a future strictness change
    // is a deliberate one.
    const body = [{ message: 'not an object' }];
    const { fetch } = mockFetchOnce(body, { status: 500 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(500);
    expect(r.error).toEqual(body);
  });

  it('keeps data undefined and status intact across error statuses', async () => {
    for (const status of [400, 401, 404, 500]) {
      const { fetch } = mockFetchOnce({ code: `e${status}` }, { status });
      const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
      const r = await sdk.getHealth();
      expect(r.status).toBe(status);
      expect(r.data).toBeUndefined();
      expect(r.error?.code).toBe(`e${status}`);
    }
  });
});
