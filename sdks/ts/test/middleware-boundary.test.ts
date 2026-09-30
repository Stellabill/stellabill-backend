/**
 * Focused boundary tests for the `options.middleware?.length` branch in
 * `createStellarBillClient` (client.ts:208).
 *
 * The branch is:
 *   if (options.middleware?.length) {
 *     for (const m of options.middleware) raw.use(m);
 *   }
 *
 * Boundary cases exercised here:
 *   1. `middleware` is `undefined`   → branch is skipped, no crash.
 *   2. `middleware` is `[]`          → `.length` is 0 (falsy), branch skipped.
 *   3. `middleware` is `[mw]`        → branch entered, single middleware runs.
 *   4. `middleware` is `[mw1, mw2]`  → both registered; insertion order preserved.
 *   5. middleware that mutates the request headers → mutation reaches fetch.
 *   6. middleware whose `onRequest` throws → error propagates to the caller.
 *   7. middleware with only `onResponse` (no `onRequest`) → no crash; hook fires.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Middleware } from 'openapi-fetch';

import { createStellarBillClient } from '../src/index.js';

// ---------------------------------------------------------------------------
// Local copies of the test helpers defined in client.test.ts so this file
// has no cross-file dependency and can be run independently.
// ---------------------------------------------------------------------------

type FetchCall = {
  url: string;
  init: RequestInit | undefined;
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

function callHeaders(call: FetchCall): Record<string, string> {
  return { ...call.initHeaders, ...call.inputHeaders };
}

function mockFetchOnce(
  body: unknown,
  init: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, { status, headers: { 'content-type': contentType } });
  const fetchFn: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      init: initArg as RequestInit | undefined,
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return res;
  });
  return { fetch: fetchFn, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const BASE = 'https://api.example.com';
const HEALTH_BODY = { status: 'ok', service: 'stellarbill-backend' };

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('middleware?.length boundary — falsy paths (branch NOT entered)', () => {
  it('case 1: middleware undefined — no extra middleware, no crash', async () => {
    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    // Explicitly pass no `middleware` key at all (same observable behaviour as undefined).
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(calls).toHaveLength(1);
  });

  it('case 1b: middleware: undefined explicit — no extra middleware, no crash', async () => {
    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: undefined });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(calls).toHaveLength(1);
  });

  it('case 2: middleware: [] (empty array) — length is 0, branch skipped, no crash', async () => {
    const mw: Middleware = {
      async onRequest({ request }) {
        // This should never be called.
        throw new Error('empty-array middleware was unexpectedly registered');
        return request;
      },
    };
    // We craft a middleware that would throw if ever invoked; the empty array
    // means it is never passed to raw.use(), so the request still succeeds.
    void mw; // silence "unused variable" without making it harder to read

    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    // Pass an empty array — the branch is NOT entered.
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [] });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(calls).toHaveLength(1);
  });
});

describe('middleware?.length boundary — truthy paths (branch IS entered)', () => {
  it('case 3: middleware: [mw] — single middleware is registered and runs', async () => {
    let ran = false;
    const mw: Middleware = {
      async onRequest({ request }) {
        ran = true;
        return request;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });
    await sdk.getHealth();

    expect(ran).toBe(true);
  });

  it('case 4a: middleware: [mw1, mw2] — both are registered', async () => {
    const called: string[] = [];
    const mw1: Middleware = {
      async onRequest({ request }) {
        called.push('mw1');
        return request;
      },
    };
    const mw2: Middleware = {
      async onRequest({ request }) {
        called.push('mw2');
        return request;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw1, mw2] });
    await sdk.getHealth();

    expect(called).toContain('mw1');
    expect(called).toContain('mw2');
  });

  it('case 4b: middleware insertion order is preserved — mw1 runs before mw2', async () => {
    /**
     * openapi-fetch runs middleware in a stack (last-registered = outermost),
     * but the SDK registers auth first, then user middleware in insertion order.
     * This test pins the observable execution order so any change to the
     * registration strategy is caught immediately.
     *
     * The Bearer token is injected by the SDK's auth middleware.  User
     * middleware runs AFTER auth (i.e. mw1 sees Bearer before mw2 does, and
     * both see it).  The `onRequest` order is mw1 → mw2; `onResponse` order
     * is the reverse: mw2 → mw1 (stack unwind).
     */
    const order: string[] = [];
    const mw1: Middleware = {
      async onRequest({ request }) {
        order.push('mw1-req');
        return request;
      },
      async onResponse({ response }) {
        order.push('mw1-res');
        return response;
      },
    };
    const mw2: Middleware = {
      async onRequest({ request }) {
        order.push('mw2-req');
        return request;
      },
      async onResponse({ response }) {
        order.push('mw2-res');
        return response;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 'tok',
      fetch,
      middleware: [mw1, mw2],
    });
    await sdk.getHealth();

    // Both request hooks must have fired.
    expect(order).toContain('mw1-req');
    expect(order).toContain('mw2-req');
    // mw1 must have run before mw2 on the request path.
    expect(order.indexOf('mw1-req')).toBeLessThan(order.indexOf('mw2-req'));
    // Both response hooks must have fired.
    expect(order).toContain('mw1-res');
    expect(order).toContain('mw2-res');
  });

  it('case 5: middleware that mutates request headers — mutation reaches fetch', async () => {
    const mw: Middleware = {
      async onRequest({ request }) {
        request.headers.set('x-injected-by-mw', 'boundary-test');
        return request;
      },
    };

    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });
    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-injected-by-mw']).toBe('boundary-test');
  });

  it('case 5b: auth header is set before user middleware runs', async () => {
    /**
     * The SDK auth middleware is registered first, so by the time user
     * middleware's onRequest fires, the Authorization header must already
     * be present on the request.  This pins the auth-before-user contract.
     */
    let bearerAtMwTime: string | null = null;
    const mw: Middleware = {
      async onRequest({ request }) {
        bearerAtMwTime = request.headers.get('authorization');
        return request;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'secret-tok', fetch, middleware: [mw] });
    await sdk.getHealth();

    expect(bearerAtMwTime).toBe('Bearer secret-tok');
  });

  it('case 6: middleware onRequest throws — error propagates to caller', async () => {
    const boom = new Error('middleware-exploded');
    const mw: Middleware = {
      async onRequest() {
        throw boom;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });

    await expect(sdk.getHealth()).rejects.toThrow('middleware-exploded');
  });

  it('case 7: middleware with only onResponse (no onRequest) — no crash', async () => {
    let responseSeen = false;
    const mw: Middleware = {
      // intentionally omit onRequest — the SDK must not require it
      async onResponse({ response }) {
        responseSeen = true;
        return response;
      },
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });
    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(responseSeen).toBe(true);
  });

  it('case 7b: middleware with only onRequest (no onResponse) — no crash on response path', async () => {
    let requestSeen = false;
    const mw: Middleware = {
      async onRequest({ request }) {
        requestSeen = true;
        return request;
      },
      // intentionally omit onResponse
    };

    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });
    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(requestSeen).toBe(true);
  });
});

describe('middleware?.length boundary — non-2xx responses still reach middleware', () => {
  it('user middleware onResponse fires even for 4xx responses', async () => {
    let statusSeen: number | undefined;
    const mw: Middleware = {
      async onResponse({ response }) {
        statusSeen = response.status;
        return response;
      },
    };

    const { fetch } = mockFetchOnce(
      { error: 'not_found', message: 'nope', code: 'x' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });
    const r = await sdk.getHealth();

    expect(r.status).toBe(404);
    expect(statusSeen).toBe(404);
  });
});
