/**
 * Focused contract tests for the middleware-registration branch in client.ts:
 *
 *   if (options.middleware?.length) {
 *     for (const m of options.middleware) raw.use(m);
 *   }
 *
 * Two sides of the branch are covered:
 *   • TRUTHY  – options.middleware is a non-empty array  → every item is
 *               registered, runs in insertion order, can mutate both the
 *               outgoing request and the incoming response, and the SDK
 *               result reflects those mutations deterministically.
 *   • FALSY   – options.middleware is [], undefined, or absent  → the branch
 *               is not entered, no extra middleware runs, and the SDK still
 *               returns clean results identical to baseline.
 *
 * These tests do NOT modify the public API or the existing middleware contract;
 * they assert the contract as it is documented.
 */

import { describe, expect, it, vi } from 'vitest';
import type { Middleware } from 'openapi-fetch';
import { createStellarBillClient } from '../src/index.js';

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

type FetchCall = {
  url: string;
  inputHeaders: Record<string, string>;
};

function buildMockFetch(
  body: unknown,
  opts: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = opts.status ?? 200;
  const contentType = opts.contentType ?? 'application/json';
  const text =
    body === undefined
      ? ''
      : typeof body === 'string'
        ? body
        : JSON.stringify(body);

  const mockFn: typeof globalThis.fetch = vi.fn(async (input) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((v, k) => {
        inputHeaders[k.toLowerCase()] = v;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), inputHeaders });
    return new Response(text, { status, headers: { 'content-type': contentType } });
  });
  return { fetch: mockFn, calls };
}

const BASE = 'https://api.example.com';
const HEALTH_BODY = { status: 'ok', service: 'stellarbill-backend' };

// ---------------------------------------------------------------------------
// TRUTHY branch – options.middleware has one or more entries
// ---------------------------------------------------------------------------

describe('middleware branch (truthy) – non-empty array is accepted and registered', () => {
  it('a single middleware with onRequest runs exactly once per SDK call', async () => {
    const onRequestCalls: string[] = [];

    const mw: Middleware = {
      async onRequest({ request }) {
        onRequestCalls.push(request.url);
        return request;
      },
    };

    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });

    await sdk.getHealth();

    expect(onRequestCalls).toHaveLength(1);
    expect(onRequestCalls[0]).toContain('/api/health');
  });

  it('a single middleware with onResponse runs exactly once per SDK call', async () => {
    const observedStatuses: number[] = [];

    const mw: Middleware = {
      async onResponse({ response }) {
        observedStatuses.push(response.status);
        return response;
      },
    };

    const { fetch } = buildMockFetch(HEALTH_BODY, { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });

    const result = await sdk.getHealth();

    // The middleware observed the response, and the SDK result reflects it.
    expect(observedStatuses).toHaveLength(1);
    expect(observedStatuses[0]).toBe(200);
    expect(result.status).toBe(200);
  });

  it('middleware can inject a custom request header and the header reaches the server', async () => {
    const TRACE_VALUE = 'test-trace-id-42';

    const mw: Middleware = {
      async onRequest({ request }) {
        request.headers.set('x-trace-id', TRACE_VALUE);
        return request;
      },
    };

    const { fetch, calls } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });

    await sdk.getHealth();

    // The header injected by middleware is present in the actual outgoing request.
    expect(calls).toHaveLength(1);
    expect(calls[0]!.inputHeaders['x-trace-id']).toBe(TRACE_VALUE);
  });

  it('middleware sees the Authorization header already set by the SDK auth middleware', async () => {
    // This asserts the documented ordering: SDK's auth middleware runs BEFORE
    // user-supplied middleware (raw.use(authMiddleware) is called first).
    let capturedAuth: string | null = null;

    const mw: Middleware = {
      async onRequest({ request }) {
        capturedAuth = request.headers.get('authorization');
        return request;
      },
    };

    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'sdk-secret-token',
      fetch,
      middleware: [mw],
    });

    await sdk.getHealth();

    // User middleware must see Bearer already set – this is the SDK's contract.
    expect(capturedAuth).toBe('Bearer sdk-secret-token');
  });

  it('multiple middleware run in insertion order on the request path', async () => {
    const order: string[] = [];

    const mw1: Middleware = {
      async onRequest({ request }) {
        order.push('mw1-req');
        return request;
      },
    };
    const mw2: Middleware = {
      async onRequest({ request }) {
        order.push('mw2-req');
        return request;
      },
    };
    const mw3: Middleware = {
      async onRequest({ request }) {
        order.push('mw3-req');
        return request;
      },
    };

    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      fetch,
      middleware: [mw1, mw2, mw3],
    });

    await sdk.getHealth();

    expect(order).toEqual(['mw1-req', 'mw2-req', 'mw3-req']);
  });

  it('multiple middleware run in reverse insertion order on the response path', async () => {
    // openapi-fetch wraps middleware as a stack, so response handlers unwind
    // in LIFO order (last-registered fires first on the way back).
    const order: string[] = [];

    const mw1: Middleware = {
      async onResponse({ response }) {
        order.push('mw1-res');
        return response;
      },
    };
    const mw2: Middleware = {
      async onResponse({ response }) {
        order.push('mw2-res');
        return response;
      },
    };

    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      fetch,
      middleware: [mw1, mw2],
    });

    await sdk.getHealth();

    // mw2 was registered last → fires first on the response path.
    expect(order).toEqual(['mw2-res', 'mw1-res']);
  });

  it('middleware header mutation is reflected in the SDK result (requestUrl, status)', async () => {
    // This is the key determinism check: the SDK's SdkResult must reflect
    // the actual network call that was made after middleware ran.
    const { fetch } = buildMockFetch(HEALTH_BODY, { status: 200 });
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'tok',
      fetch,
      middleware: [
        {
          async onRequest({ request }) {
            request.headers.set('x-sdk-test', 'yes');
            return request;
          },
        },
      ],
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
    expect(result.error).toBeUndefined();
    expect(result.data?.status).toBe('ok');
  });

  it('middleware that returns a synthetic error response is propagated to SdkResult.error', async () => {
    // A middleware can short-circuit the call by returning a custom Response
    // from onRequest. The SDK must surface this as a non-2xx SdkResult.
    const syntheticBody = { error: 'Blocked', message: 'blocked by middleware', code: 'mw_block' };

    const blockingMw: Middleware = {
      async onRequest() {
        return new Response(JSON.stringify(syntheticBody), {
          status: 403,
          headers: { 'content-type': 'application/json' },
        });
      },
    };

    // The real fetch should never be called since the middleware short-circuits.
    const { fetch, calls } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      fetch,
      middleware: [blockingMw],
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(403);
    // openapi-fetch will parse the JSON error from the synthetic response.
    expect(result.error).toBeDefined();
    // The real fetch was never invoked – the middleware owned the response.
    expect(calls).toHaveLength(0);
  });

  it('middleware that augments a non-2xx response is forwarded cleanly to throwOnError', async () => {
    const mw: Middleware = {
      async onResponse({ response }) {
        // Just pass through – we're verifying middleware + throwOnError compose.
        return response;
      },
    };

    const { fetch } = buildMockFetch(
      { error: 'Not Found', message: 'not found', code: 'resource_missing' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch,
      middleware: [mw],
    });

    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 404,
      requestMethod: 'GET',
    });
  });

  it('middleware runs on every SDK method, not only getHealth', async () => {
    const calledUrls: string[] = [];

    const mw: Middleware = {
      async onRequest({ request }) {
        calledUrls.push(new URL(request.url).pathname);
        return request;
      },
    };

    const { fetch: f1 } = buildMockFetch({ plans: [], pagination: { has_more: false } });
    const sdk1 = createStellarBillClient({ baseUrl: BASE, fetch: f1, middleware: [mw] });
    await sdk1.listPlans();

    const { fetch: f2 } = buildMockFetch({ subscriptions: [], pagination: { has_more: false } });
    const sdk2 = createStellarBillClient({ baseUrl: BASE, fetch: f2, middleware: [mw] });
    await sdk2.listSubscriptions();

    expect(calledUrls).toContain('/api/v1/plans');
    expect(calledUrls).toContain('/api/subscriptions');
  });
});

// ---------------------------------------------------------------------------
// FALSY branch – options.middleware is [], undefined, or absent
// ---------------------------------------------------------------------------

describe('middleware branch (falsy) – empty/absent array is accepted and skipped', () => {
  it('options.middleware: [] is accepted without error and SDK works normally', async () => {
    const { fetch, calls } = buildMockFetch(HEALTH_BODY);

    // Must not throw, must produce a clean 200.
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [] });
    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.data?.status).toBe('ok');
    expect(calls).toHaveLength(1);
  });

  it('options.middleware: undefined is accepted without error and SDK works normally', async () => {
    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: undefined });
    const result = await sdk.getHealth();
    expect(result.status).toBe(200);
  });

  it('options.middleware absent (not provided) is accepted without error and SDK works normally', async () => {
    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(200);
  });

  it('with [] middleware the auth Bearer header is still injected (SDK-installed auth is unaffected)', async () => {
    const { fetch, calls } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'baseline-token',
      fetch,
      middleware: [],
    });

    await sdk.getHealth();

    expect(calls[0]!.inputHeaders['authorization']).toBe('Bearer baseline-token');
  });

  it('with undefined middleware the auth Bearer header is still injected', async () => {
    const { fetch, calls } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'baseline-token',
      fetch,
      middleware: undefined,
    });

    await sdk.getHealth();

    expect(calls[0]!.inputHeaders['authorization']).toBe('Bearer baseline-token');
  });

  it('SDK result is identical whether middleware is [] or absent', async () => {
    const { fetch: f1 } = buildMockFetch(HEALTH_BODY);
    const sdk1 = createStellarBillClient({ baseUrl: BASE, token: 't', fetch: f1, middleware: [] });
    const r1 = await sdk1.getHealth();

    const { fetch: f2 } = buildMockFetch(HEALTH_BODY);
    const sdk2 = createStellarBillClient({ baseUrl: BASE, token: 't', fetch: f2 });
    const r2 = await sdk2.getHealth();

    // Status, data, and error must be identical.
    expect(r1.status).toBe(r2.status);
    expect(r1.data?.status).toBe(r2.data?.status);
    expect(r1.error).toBe(r2.error);
  });

  it('empty middleware array does not prevent throwOnError from working', async () => {
    const { fetch } = buildMockFetch(
      { error: 'Forbidden', message: 'access denied', code: 'forbidden' },
      { status: 403 },
    );
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      throwOnError: true,
      fetch,
      middleware: [],
    });

    await expect(sdk.getHealth()).rejects.toMatchObject({ status: 403 });
  });

  it('empty middleware array does not suppress setToken / getToken', async () => {
    const { fetch } = buildMockFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [] });

    sdk.setToken('rotated');
    expect(sdk.getToken()).toBe('rotated');

    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
  });
});
