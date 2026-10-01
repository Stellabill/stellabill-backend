import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  assertOk,
  createStellarBillClient,
  StellarBillError,
} from '../src/index.js';

// ─── mock fetch ─────────────────────────────────────────────────────────────

function respondingFetch(body: unknown, status: number, contentType = 'application/json'): {
  fetch: typeof globalThis.fetch;
} {
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const fetch = vi.fn(
    async () =>
      new Response(text, { status, headers: { 'content-type': contentType } }),
  );
  return { fetch: fetch as unknown as typeof globalThis.fetch };
}

afterEach(() => {
  vi.restoreAllMocks();
});

// ─── SdkResult envelope built by `wrap` ─────────────────────────────────────

describe('SdkResult envelope - 2xx', () => {
  it('returns parsed data, an undefined error and the transport metadata', async () => {
    const { fetch } = respondingFetch({ status: 'ok', service: 'stellarbill-backend' }, 200);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();

    expect(r.data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
    expect(r.error).toBeUndefined();
    expect(r.status).toBe(200);
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toBe('/api/health');
    expect(r.response).toBeInstanceOf(Response);
    expect(r.response.status).toBe(200);
  });
});

describe('SdkResult envelope - non-2xx', () => {
  it('surfaces a JSON object error body without throwing', async () => {
    const body = { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' };
    const { fetch } = respondingFetch(body, 400);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.listPlans({ limit: 999 });

    expect(r.status).toBe(400);
    expect(r.data).toBeUndefined();
    expect(r.error).toEqual(body);
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toBe('/api/v1/plans');
  });

  it('returns an undefined error when the non-2xx body is a bare primitive', async () => {
    const { fetch } = respondingFetch('plain failure', 502, 'text/plain');
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(502);
    expect(r.data).toBeUndefined();
    // `typeof "plain failure" !== 'object'`, so parsedError must be undefined.
    expect(r.error).toBeUndefined();
    expect(r.requestUrl).toBe('/api/health');
  });

  it('returns an undefined error when the non-2xx body is empty', async () => {
    const { fetch } = respondingFetch('', 500);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(500);
    expect(r.error).toBeUndefined();
  });

  it('passes a JSON array error body through unchanged (typeof [] is object)', async () => {
    const { fetch } = respondingFetch([{ message: 'a' }], 400);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(400);
    expect(r.error).toEqual([{ message: 'a' }]);
  });
});

// ─── throwOnError path ──────────────────────────────────────────────────────

describe('throwOnError - StellarBillError contract', () => {
  it('carries status, body, method, url and a descriptive message', async () => {
    const body = { error: 'Not Found', message: 'subscription is gone', code: 'missing' };
    const { fetch } = respondingFetch(body, 404);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    let caught: unknown;
    try {
      await sdk.getSubscription('missing-id');
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(StellarBillError);
    const e = caught as StellarBillError;
    expect(e.status).toBe(404);
    expect(e.body).toEqual(body);
    expect(e.requestMethod).toBe('GET');
    expect(e.requestUrl).toBe('/api/subscriptions/missing-id');
    expect(e.message).toContain('GET');
    expect(e.message).toContain('/api/subscriptions/missing-id');
    expect(e.message).toContain('404');
    expect(e.message).toContain('subscription is gone');
  });

  it('falls back to body.error when body.message is absent', async () => {
    const { fetch } = respondingFetch({ error: 'Unauthorized' }, 401);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    await expect(sdk.getHealth()).rejects.toThrow(/Unauthorized/);
  });

  it('falls back to "HTTP <status>" when the body has neither message nor error', async () => {
    const { fetch } = respondingFetch({}, 503);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    await expect(sdk.getHealth()).rejects.toThrow(/HTTP 503/);
  });
});

// ─── assertOk over the same envelope ────────────────────────────────────────

describe('assertOk - envelope consumption', () => {
  it('returns data for a 2xx envelope', async () => {
    const { fetch } = respondingFetch({ status: 'ok' }, 200);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await expect(assertOk(await sdk.getHealth())).resolves.toEqual({ status: 'ok' });
  });

  it('throws a StellarBillError that mirrors the non-2xx envelope', async () => {
    const { fetch } = respondingFetch({ message: 'nope' }, 500);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const r = await sdk.getHealth();
    await expect(assertOk(r)).rejects.toMatchObject({ status: 500, body: { message: 'nope' } });
  });
});
