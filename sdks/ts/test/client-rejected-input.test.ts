import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  createStellarBillClient,
  StellarBillConfigError,
  StellarBillError,
} from '../src/index.js';

/**
 * Dedicated coverage for the documented control-flow branch in
 * `src/client.ts` around `throwOnError`:
 *
 *   "When `true`, throw {@link StellarBillError} on any non-2xx response."
 *
 * These tests drive a request whose input the API rejects and assert that it
 * reaches the intended rejection path with a stable, actionable error
 * contract (status / body / method / url / message / toString), that the
 * `throwOnError: false` default still exposes the same contract without
 * throwing, and that auth-token handling on the rejected request is
 * deterministic.
 */

type RecordedCall = {
  url: string;
  /** Headers observed by the mock fetch (Request input merged with init.headers). */
  headers: Record<string, string>;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

/**
 * Fetch stub that returns a fresh `Response` per call (so a single client can
 * make several calls, e.g. rejected-then-successful), recording the headers
 * each outbound request actually carried.
 */
function queueFetch(responses: Array<() => Response>): {
  fetch: typeof globalThis.fetch;
  calls: RecordedCall[];
} {
  const calls: RecordedCall[] = [];
  let index = 0;
  const fetchImpl: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const headers: Record<string, string> = {};
    if (initArg?.headers) {
      new Headers(initArg.headers).forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: toUrl(input), headers });

    const factory = responses[Math.min(index, responses.length - 1)]!;
    index += 1;
    return factory();
  });
  return { fetch: fetchImpl, calls };
}

function jsonResponse(body: unknown, status: number): Response {
  return new Response(body === undefined ? '' : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

/** Capture the value a promise rejects with, without throwing the test. */
async function captureRejection(promise: Promise<unknown>): Promise<unknown> {
  try {
    await promise;
    throw new Error('expected promise to reject, but it resolved');
  } catch (err) {
    return err;
  }
}

afterEach(() => {
  vi.restoreAllMocks();
});

const VALIDATION_BODY = {
  error: 'Unprocessable Entity',
  message: 'limit must be between 1 and 100',
  code: 'validation_error',
};

describe('rejected input via throwOnError (src/client.ts)', () => {
  it('throws StellarBillError with a stable, actionable contract on a 422 rejection', async () => {
    const { fetch, calls } = queueFetch([() => jsonResponse(VALIDATION_BODY, 422)]);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok-abc',
      throwOnError: true,
      fetch,
    });

    const caught = await captureRejection(sdk.listPlans({ limit: 999 }));

    expect(caught).toBeInstanceOf(StellarBillError);
    const err = caught as StellarBillError;
    expect(err.name).toBe('StellarBillError');
    expect(err).toBeInstanceOf(Error);
    // Stable machine-readable fields.
    expect(err.status).toBe(422);
    expect(err.requestMethod).toBe('GET');
    expect(err.requestUrl).toBe('/api/v1/plans');
    expect(err.body).toEqual(VALIDATION_BODY);
    // Actionable human-readable message: method + url + status + server detail.
    expect(err.message).toBe('GET /api/v1/plans failed (422): limit must be between 1 and 100');
    // toString carries the API error code for logs.
    expect(err.toString()).toContain('validation_error');

    // The rejected request still carried auth: the rejection is attributable
    // to the input, not to a missing token.
    expect(calls).toHaveLength(1);
    expect(calls[0]!.headers['authorization']).toBe('Bearer tok-abc');
    // The failure did not mutate the token holder.
    expect(sdk.getToken()).toBe('tok-abc');
  });

  it('does not throw by default but still exposes the same rejected-input envelope', async () => {
    const { fetch } = queueFetch([() => jsonResponse(VALIDATION_BODY, 422)]);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.listPlans({ limit: 999 });

    expect(result.status).toBe(422);
    expect(result.data).toBeUndefined();
    expect(result.error).toEqual(VALIDATION_BODY);
    expect(result.response.status).toBe(422);
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/v1/plans');
  });

  it('does not throw on a 2xx response even when throwOnError is enabled', async () => {
    const payload = { plans: [], pagination: { has_more: false } };
    const { fetch } = queueFetch([() => jsonResponse(payload, 200)]);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const result = await sdk.listPlans();

    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data).toEqual(payload);
  });

  it('keeps the thrown error actionable when the rejection carries no JSON body', async () => {
    const { fetch } = queueFetch([
      () => new Response('<html>nope</html>', { status: 400, headers: { 'content-type': 'text/html' } }),
    ]);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const caught = await captureRejection(sdk.getSubscription('sub-1'));

    expect(caught).toBeInstanceOf(StellarBillError);
    const err = caught as StellarBillError;
    expect(err.status).toBe(400);
    expect(err.body).toBeUndefined();
    // Falls back to `HTTP <status>` rather than an empty/unhelpful message.
    expect(err.message).toBe('GET /api/subscriptions/sub-1 failed (400): HTTP 400');
    expect(err.toString()).toContain('(unknown)');
  });

  it('falls back to the body `error` field when `message` is absent', async () => {
    const { fetch } = queueFetch([
      () => jsonResponse({ error: 'rate_limited', code: 'rate_limited' }, 429),
    ]);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const caught = await captureRejection(sdk.listSubscriptions());

    const err = caught as StellarBillError;
    expect(err.status).toBe(429);
    expect(err.body).toEqual({ error: 'rate_limited', code: 'rate_limited' });
    expect(err.message).toBe('GET /api/subscriptions failed (429): rate_limited');
  });
});

describe('rejected-input token behavior', () => {
  it('authenticates the rejected request and keeps the token usable afterwards', async () => {
    const { fetch, calls } = queueFetch([
      () => jsonResponse({ error: 'Unauthorized', message: 'expired', code: 'auth_unauthorized' }, 401),
      () => jsonResponse({ status: 'ok', service: 'stellarbill-backend' }, 200),
      () => jsonResponse({ status: 'ok', service: 'stellarbill-backend' }, 200),
    ]);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok-1',
      throwOnError: true,
      fetch,
    });

    const caught = await captureRejection(sdk.getHealth());
    expect(caught).toBeInstanceOf(StellarBillError);
    expect((caught as StellarBillError).status).toBe(401);
    expect(calls[0]!.headers['authorization']).toBe('Bearer tok-1');

    // The rejected call must not clear or corrupt the token holder.
    expect(sdk.getToken()).toBe('tok-1');
    const ok = await sdk.getHealth();
    expect(ok.status).toBe(200);
    expect(calls[1]!.headers['authorization']).toBe('Bearer tok-1');

    // ...and clearing the token is reflected on the next outbound request.
    sdk.setToken(undefined);
    await sdk.getHealth();
    expect(calls[2]!.headers['authorization']).toBeUndefined();
    expect(calls).toHaveLength(3);
  });
});

describe('client-side rejected input (no request issued)', () => {
  it('rejects invalid arguments deterministically before any network call', async () => {
    const { fetch, calls } = queueFetch([() => jsonResponse({ status: 'ok' }, 200)]);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await expect(sdk.getSubscription('')).rejects.toBeInstanceOf(StellarBillConfigError);
    await expect(sdk.getSubscription(undefined as unknown as string)).rejects.toBeInstanceOf(
      StellarBillConfigError,
    );
    await expect(sdk.inspectIdempotencyKey('')).rejects.toBeInstanceOf(StellarBillConfigError);
    await expect(sdk.inspectIdempotencyKey('x'.repeat(256))).rejects.toBeInstanceOf(
      StellarBillConfigError,
    );
    // Invalid input is rejected locally; the transport is never touched.
    expect(calls).toHaveLength(0);
  });
});
