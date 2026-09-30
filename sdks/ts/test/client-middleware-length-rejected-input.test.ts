/**
 * Regression guard for rejected/edge input at the `options.middleware?.length`
 * branch in `createStellarBillClient` (issue #951, sdks/ts/src/client.ts — the
 * options.middleware?.length branch).
 *
 * The branch is:
 *   if (options.middleware?.length) {
 *     for (const m of options.middleware) raw.use(m);
 *   }
 *
 * The sibling `test/middleware-boundary.test.ts` already covers the happy-path
 * behaviour of this branch.  This file deliberately focuses on the rejection
 * paths: inputs that must NOT register middleware, inputs whose failure must
 * reach the caller, and the non-array boundary where `for...of` rejects.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Middleware } from 'openapi-fetch';

import { createStellarBillClient } from '../src/index.js';

// ---------------------------------------------------------------------------
// Local test helpers (copied from test/middleware-boundary.test.ts so this file
// has no cross-file dependency and can be run independently).
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
// Fixtures
// ---------------------------------------------------------------------------

const BASE = 'https://api.example.com';
const HEALTH_BODY = { status: 'ok', service: 'stellarbill-backend' };

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('middleware?.length rejected-input coverage (#951)', () => {
  it('success: middleware omitted — branch is not entered and the request succeeds', async () => {
    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(r.data).toEqual(HEALTH_BODY);
    expect(calls).toHaveLength(1);
  });

  it('success: middleware: [] — length 0 skips the branch, request still succeeds', async () => {
    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [] });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(calls).toHaveLength(1);
  });

  it('failure: a throwing middleware inside a non-empty array is registered and its error reaches the caller', async () => {
    const boom = new Error('rejected-input-middleware-exploded');
    const throwingMw: Middleware = {
      async onRequest() {
        throw boom;
      },
    };

    // The array is non-empty, so the branch IS entered and raw.use() registers
    // the middleware; the throw must propagate out of sdk.getHealth().
    const { fetch } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [throwingMw] });

    await expect(sdk.getHealth()).rejects.toThrow('rejected-input-middleware-exploded');
  });

  it('success (control): a valid middleware runs and its header mutation reaches fetch', async () => {
    let ran = false;
    const mw: Middleware = {
      async onRequest({ request }) {
        ran = true;
        request.headers.set('x-rejected-input-test', 'ran');
        return request;
      },
    };

    const { fetch, calls } = mockFetchOnce(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, middleware: [mw] });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect(ran).toBe(true);
    expect(callHeaders(calls[0]!)['x-rejected-input-test']).toBe('ran');
  });

  it('boundary: a non-array object with a truthy length rejects (for...of on non-iterable)', () => {
    const { fetch } = mockFetchOnce(HEALTH_BODY);

    // The `?.length` guard is truthy for `{ length: 1 }`, so `for...of` runs on
    // a non-iterable and throws.  Because the branch lives in the client
    // factory, the rejection surfaces synchronously from
    // `createStellarBillClient` — it never gets as far as `sdk.getHealth()`.
    expect(() =>
      createStellarBillClient({
        baseUrl: BASE,
        fetch,
        middleware: { length: 1 } as unknown as Middleware[],
      }),
    ).toThrow();
  });
});
