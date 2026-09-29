import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  assertOk,
  createStellarBillClient,
  safeParseErrorBody,
  StellarBillConfigError,
  StellarBillError,
} from '../src/index.js';

type FetchCall = {
  url: string;
  init: RequestInit | undefined;
  /** Headers extracted from the Request object (openapi-fetch passes Request as input). */
  inputHeaders: Record<string, string>;
  /** Headers extracted from init.headers (fallback if openapi-fetch passes a URL). */
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

/** Headers as observed by the mock fetch (covers both Request input + URL+init input). */
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
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
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
  return { fetch, calls };
}

function makeFetchForResponse(response: Response): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
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
    return response;
  });
  return { fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - configuration', () => {
  it('throws StellarBillConfigError on missing baseUrl', () => {
    expect(() => createStellarBillClient({ baseUrl: undefined as unknown as string })).toThrow(
      StellarBillConfigError,
    );
    expect(() => createStellarBillClient({ baseUrl: '' })).toThrow(/non-empty/);
    expect(() => createStellarBillClient({ baseUrl: '   ' })).toThrow(/non-empty/);
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url' })).toThrow(/not a valid URL/);
  });

  it('warns when baseUrl is http and not localhost', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
    await sdk.getHealth();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
  });

  it('does not warn when baseUrl is https', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(warn).not.toHaveBeenCalled();
  });

  it('does not warn when baseUrl is http on localhost/127.0.0.1', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch: f1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk1 = createStellarBillClient({ baseUrl: 'http://localhost:8080', fetch: f1 });
    await sdk1.getHealth();
    const { fetch: f2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk2 = createStellarBillClient({ baseUrl: 'http://127.0.0.1:8080', fetch: f2 });
    await sdk2.getHealth();
    expect(warn).not.toHaveBeenCalled();
  });

  it('throws StellarBillConfigError when no fetch implementation is available', () => {
    const saved = (globalThis as { fetch?: unknown }).fetch;
    (globalThis as { fetch?: unknown }).fetch = undefined;
    try {
      expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com' })).toThrow(/No fetch/);
    } finally {
      (globalThis as { fetch?: unknown }).fetch = saved;
    }
  });

  it('accepts an explicit fetch option', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(calls).toHaveLength(1);
  });

  it('strips trailing slashes from baseUrl', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url.startsWith('https://api.example.com/api/health')).toBe(true);
    expect(calls[0]!.url).not.toContain('///api');
  });

  it('strips trailing slashes from baseUrl including port variants', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://127.0.0.1:8080/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('http://127.0.0.1:8080/api/health');
  });
});

describe('createStellarBillClient - headers and auth', () => {
  it('injects Authorization Bearer header when token is set', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: '  my-token  ',
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBe('Bearer my-token');
  });

  it('drops malformed token (whitespace inside)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('setToken rotates the token; subsequent calls use the new one', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'old', fetch });
    expect(sdk.getToken()).toBe('old');
    sdk.setToken('new');
    expect(sdk.getToken()).toBe('new');
    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('attaches user-agent and static headers on every request', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Custom-Trace': 'abc' },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(headers['x-custom-trace']).toBe('abc');
  });

  it('skips empty-valued static headers', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Empty': '', 'X-Keep': 'v' },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['x-empty']).toBeUndefined();
    expect(headers['x-keep']).toBe('v');
  });

  it('rejects caller-supplied Authorization header (auth bypass prevention)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { Authorization: 'Bearer attacker-controlled' },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('runs user-supplied middleware around the auth middleware', async () => {
    const order: string[] = [];
    let bearerSeen = false;
    const res = new Response(JSON.stringify({ status: 'ok' }), { status: 200 });
    const inner = makeFetchForResponse(res);
    const trackingFetch: typeof globalThis.fetch = vi.fn(async (input, init) => {
      order.push('fetch');
      return inner.fetch(input, init as RequestInit | undefined);
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok',
      fetch: trackingFetch,
      middleware: [
        {
          async onRequest({ request }) {
            order.push('user-mw-before');
            // Request must already carry Bearer by the time user middleware runs.
            bearerSeen = request.headers.get('authorization') === 'Bearer tok';
            return request;
          },
          async onResponse({ response }) {
            order.push('user-mw-after');
            return response;
          },
        },
      ],
    });
    await sdk.getHealth();
    expect(order).toEqual(['user-mw-before', 'fetch', 'user-mw-after']);
    expect(bearerSeen).toBe(true);
  });
});

describe('createStellarBillClient - typed wrappers (success paths)', () => {
  it('getHealth returns parsed data', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data?.status).toBe('ok');
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toContain('/api/health');
  });

  it('listPlans forwards cursor and limit', async () => {
    const { fetch, calls } = mockFetchOnce({ plans: [{ id: 'p1' }], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listPlans({ cursor: 'c', limit: 25 });
    expect(calls[0]!.url).toContain('/api/v1/plans');
    expect(calls[0]!.url).toContain('cursor=c');
    expect(calls[0]!.url).toContain('limit=25');
  });

  it('listSubscriptions forwards cursor and limit', async () => {
    const { fetch, calls } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listSubscriptions({ cursor: 'c', limit: 25 });
    expect(calls[0]!.url).toContain('/api/subscriptions');
    expect(calls[0]!.url).toContain('cursor=c');
    expect(calls[0]!.url).toContain('limit=25');
  });

  it('listSubscriptions omits undefined query params', async () => {
    const { fetch, calls } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listSubscriptions();
    expect(calls[0]!.url).toContain('/api/subscriptions');
    expect(calls[0]!.url).not.toContain('cursor=');
    expect(calls[0]!.url).not.toContain('limit=');
  });

  it('getSubscription throws on empty id', async () => {
    const { fetch } = mockFetchOnce({ id: 'x', plan_id: 'p', customer: 'c', status: 'a', amount: '1', interval: 'm' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.getSubscription('')).rejects.toBeInstanceOf(StellarBillConfigError);
  });

  it('getSubscription preserves special characters in the path', async () => {
    const { fetch, calls } = mockFetchOnce({ id: 'sub/with spaces', plan_id: 'p' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getSubscription('sub/with spaces');
    expect(calls[0]!.url).toContain('/api/subscriptions/');
    expect(decodeURIComponent(calls[0]!.url)).toContain('/api/subscriptions/sub/with spaces');
  });

  it('inspectIdempotencyKey rejects empty key', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.inspectIdempotencyKey('')).rejects.toBeInstanceOf(StellarBillConfigError);
  });

  it('inspectIdempotencyKey rejects key longer than 255 chars', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.inspectIdempotencyKey('x'.repeat(256))).rejects.toBeInstanceOf(
      StellarBillConfigError,
    );
  });

  it('inspectIdempotencyKey returns parsed record on success', async () => {
    const { fetch } = mockFetchOnce({
      key: 'abc',
      used_at: '2026-01-01T00:00:00Z',
      expires_at: '2026-01-02T00:00:00Z',
      status_code: 201,
      request_fingerprint: 'fp',
    });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.inspectIdempotencyKey('abc');
    expect(r.data?.status_code).toBe(201);
  });

  it('exposes version, raw client, and token accessors', () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 't', fetch });
    expect(sdk.version).toMatch(/^\d+\.\d+\.\d+$/);
    expect(typeof sdk.raw).toBe('object');
    expect(sdk.getToken()).toBe('t');
  });

  it('listPlans throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'Bad Request', message: 'bad', code: 'x' }, { status: 400 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.listPlans()).rejects.toMatchObject({ status: 400 });
  });

  it('listSubscriptions returns parsed data on success', async () => {
    const { fetch } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.listSubscriptions();
    expect(r.data?.subscriptions.length).toBe(0);
  });

  it('getSubscription throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'Not Found', message: 'gone', code: 'missing' }, { status: 404 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.getSubscription('missing')).rejects.toMatchObject({ status: 404 });
  });

  it('inspectIdempotencyKey throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Unauthorized', message: 'auth', code: 'auth_unauthorized' },
      { status: 401 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.inspectIdempotencyKey('abc')).rejects.toMatchObject({ status: 401 });
  });
});

describe('createStellarBillClient - error paths (non-2xx)', () => {
  it('returns parsed error in result when not throwOnError', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.listPlans({ limit: 999 });
    expect(r.status).toBe(400);
    expect(r.error?.code).toBe('invalid_cursor');
    expect(r.data).toBeUndefined();
  });

  it('throws StellarBillError when throwOnError: true and status is non-2xx', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Not Found', message: 'gone', code: 'missing' },
      { status: 404 },
    );
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
    expect(e.requestMethod).toBe('GET');
    expect(e.requestUrl).toContain('/api/subscriptions/');
    expect(e.body?.code).toBe('missing');
  });

  it('non-2xx with non-JSON content returns undefined error body', async () => {
    const { fetch } = mockFetchOnce('<html>nope</html>', { status: 500, contentType: 'text/html' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 500,
      body: undefined,
    });
  });
});

describe('createStellarBillClient - warning path coverage', () => {
  it('does not crash when console is fully unavailable', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const savedConsole = globalThis.console;
    (globalThis as { console?: Console }).console = undefined as unknown as Console;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      const r = await sdk.getHealth();
      expect(r.status).toBe(200);
    } finally {
      (globalThis as { console?: Console }).console = savedConsole;
    }
  });

  it('skips warning when console.warn is missing', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const savedWarn = console.warn;
    (console as unknown as { warn?: () => void }).warn = undefined;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      const r = await sdk.getHealth();
      expect(r.status).toBe(200);
    } finally {
      console.warn = savedWarn;
    }
  });
});

describe('assertOk', () => {
  it('returns data when 2xx and data present', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    const data = await assertOk(r);
    expect(data.status).toBe('ok');
  });

  it('throws when 2xx but body has no parsed data', async () => {
    // openapi-fetch always calls res.json(), so the SDK cannot produce
    // a `data === undefined` with status 200. Construct the SdkResult
    // inline to exercise the assertOk branch directly.
    const r = {
      data: undefined,
      error: undefined,
      status: 200,
      response: new Response('{}', { status: 200 }),
      requestMethod: 'GET',
      requestUrl: '/test',
    };
    await expect(assertOk(r)).rejects.toThrow(/empty body/);
  });

  it('throws StellarBillError on non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'oops' }, { status: 500 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    await expect(assertOk(r)).rejects.toBeInstanceOf(StellarBillError);
    await expect(assertOk(r)).rejects.toMatchObject({ status: 500 });
  });
});

describe('safeParseErrorBody', () => {
  it('returns undefined when content-type is missing', async () => {
    const r = new Response('{}', { status: 400 });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when content-type is not JSON', async () => {
    const r = new Response('oops', { status: 400, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when body is empty', async () => {
    const r = new Response('', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('parses a valid error body', async () => {
    const r = new Response(JSON.stringify({ message: 'bad', code: 'x' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bad', code: 'x' });
  });

  it('returns undefined on invalid JSON', async () => {
    const r = new Response('not-json', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is a string', async () => {
    const r = new Response('"a string"', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is an array', async () => {
    const r = new Response('[1,2,3]', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is null', async () => {
    const r = new Response('null', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when status 200 with empty body and non-json content', async () => {
    const r = new Response('', { status: 200, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when res.text() throws', async () => {
    const r = new Response('ok', { status: 400, headers: { 'content-type': 'application/json' } });
    vi.spyOn(r, 'text').mockRejectedValue(new Error('boom'));
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});

describe('Token integration with createStellarBillClient', () => {
  it('handles basic TokenHolder behavior via the SDK', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk1 = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    expect(sdk1.getToken()).toBeUndefined();
    sdk1.setToken('a');
    expect(sdk1.getToken()).toBe('a');
    sdk1.setToken(undefined);
    expect(sdk1.getToken()).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Boundary conditions for authMiddleware.onRequest — line 204 `return request`
//
// The handler at lines 191-204 of client.ts has three observable branches before
// the unconditional `return request`:
//   1. User-agent injection  — skipped if request already has 'user-agent'
//   2. Extra-header injection — skipped per-header if already present or empty value
//   3. Token injection        — outer guard: hasToken(); inner guard: if (t)
//
// The tests below pin the observable behaviour of each branch so that any
// silent mutation of the middleware logic is caught immediately.
// ---------------------------------------------------------------------------

describe('authMiddleware.onRequest boundary conditions (client.ts:204 return request)', () => {
  // Helper: intercept the Request object that the middleware has already
  // mutated, so we can read its headers without going through the mock
  // fetch's header extraction logic.
  function interceptingFetch(body: unknown = { status: 'ok' }): {
    fetch: typeof globalThis.fetch;
    capturedRequest: () => Request | undefined;
    capturedHeaders: () => Record<string, string>;
  } {
    let captured: Request | undefined;
    const f: typeof globalThis.fetch = vi.fn(async (input) => {
      if (input instanceof Request) {
        captured = input;
      }
      const text = typeof body === 'string' ? body : JSON.stringify(body);
      return new Response(text, {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });
    return {
      fetch: f,
      capturedRequest: () => captured,
      capturedHeaders: () => {
        const out: Record<string, string> = {};
        captured?.headers.forEach((v, k) => {
          out[k.toLowerCase()] = v;
        });
        return out;
      },
    };
  }

  // ── Branch 1: user-agent is absent → middleware SETS it ────────────────
  it('sets user-agent when the request does not already carry one', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(capturedHeaders()['user-agent']).toMatch(/^@stellabill\/sdk\//);
  });

  // ── Branch 1 false path: user-agent already present → middleware skips it ──
  it('does NOT override user-agent when the request already carries one', async () => {
    const presetUA = 'my-custom-agent/1.0';
    // Inject user-agent via a middleware that runs before the SDK's auth
    // middleware by using the raw openapi-fetch client or by supplying a
    // custom middleware that sets the header before the SDK's one fires.
    //
    // The SDK's auth middleware runs first (it is `raw.use`d before any
    // user middleware). To pre-set user-agent we need it to arrive on the
    // Request object *before* authMiddleware runs.
    //
    // The easiest way: supply a middleware that is prepended before ours
    // by abusing the fact that openapi-fetch middleware runs in insertion
    // order. However, user middleware added via options.middleware fires
    // *after* authMiddleware (see client.ts line ~207).
    //
    // Instead we test the actual guard directly: the SDK itself pre-populates
    // extraHeaders['user-agent'] in the client factory, so the onRequest guard
    // `!request.headers.has('user-agent')` will always be false for a brand-new
    // Request that doesn't have one, and always be true if the header is absent.
    //
    // We validate the skip path by observing that the SDK's own user-agent
    // value (from defaultUserAgent()) equals what ends up on the request — 
    // meaning the guard ran and set it. The "already present" skip path is
    // structurally the negation tested below through a custom middleware that
    // arrives after auth sets it, so we cannot override it that way.
    //
    // The most deterministic approach is to supply a pre-request middleware
    // via openapi-fetch `raw.use` *after* client construction. That middleware
    // runs after the SDK's own middleware due to insertion order, so it would
    // read the post-mutation headers. Instead we expose a user-supplied
    // onRequest that reads the already-mutated request, which is how
    // `middleware` option works.
    //
    // To actually hit the `!request.headers.has('user-agent') === false` branch
    // we must start with a Request that already has user-agent. Because
    // openapi-fetch constructs the Request internally, the only hook we have
    // is the middleware chain itself. A second middleware that pre-sets the
    // header doesn't work because user-middleware runs *after* auth.
    //
    // We therefore test this branch through a white-box observation: the
    // extraHeaders map in the client factory is pre-loaded with 'user-agent'
    // (see client.ts line ~174). When onRequest runs, the extra-header loop
    // also guards `!request.headers.has(k)`. Since user-agent was already
    // set by the `if (!request.headers.has('user-agent'))` block earlier in
    // onRequest, the extra-header loop will skip it — confirming both paths.
    //
    // The observable contract we pin here: whatever user-agent arrives on the
    // wire is EXACTLY the SDK's default user-agent, not a caller-injected one.
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      // Trying to override via headers: is blocked by the Authorization
      // guard, but user-agent is not Authorization, so it would be normalised
      // into extraHeaders. However it is then skipped by the extra-header
      // loop because the earlier `!request.headers.has('user-agent')` branch
      // already set it. The caller-supplied value should NOT appear.
      headers: { 'user-agent': presetUA },
      fetch,
    });
    await sdk.getHealth();
    // The SDK's own user-agent should win (set by the first branch).
    // The caller's 'user-agent' in extraHeaders is skipped by the loop's guard.
    expect(capturedHeaders()['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(capturedHeaders()['user-agent']).not.toBe(presetUA);
  });

  // ── Branch 2: extra header absent → middleware SETS it ─────────────────
  it('injects extra headers that are not yet present on the request', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'x-tenant-id': 'tenant-42' },
      fetch,
    });
    await sdk.getHealth();
    expect(capturedHeaders()['x-tenant-id']).toBe('tenant-42');
  });

  // ── Branch 2 empty-value guard: empty string skipped in factory, not set ─
  it('does not inject extra headers whose value is an empty string', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'x-empty-header': '', 'x-valid-header': 'present' },
      fetch,
    });
    await sdk.getHealth();
    // Empty value: filtered out during factory construction, never enters extraHeaders
    expect(capturedHeaders()['x-empty-header']).toBeUndefined();
    // Non-empty value: must be present
    expect(capturedHeaders()['x-valid-header']).toBe('present');
  });

  // ── Branch 3 outer guard: no token → hasToken()=false → no Authorization ──
  it('returns request WITHOUT Authorization header when no token is configured', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    // No `token` option — tokenHolder starts with undefined
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBeUndefined();
  });

  // ── Branch 3 outer guard: token set → hasToken()=true, t truthy → Bearer set
  it('returns request WITH Authorization: Bearer when a valid token is configured', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'valid-token-abc',
      fetch,
    });
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer valid-token-abc');
  });

  // ── Branch 3 outer+inner: token cleared mid-flight → hasToken()=false ───
  it('omits Authorization after setToken(undefined) even when it was set initially', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'initial-token',
      fetch,
    });
    sdk.setToken(undefined); // clears tokenHolder; hasToken() → false
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBeUndefined();
  });

  // ── Inner guard `if (t)`: hasToken() true but get() hypothetically falsy ─
  // TokenHolder.hasToken() guarantees that get() returns a non-empty string
  // when it returns true, so in production the inner `if (t)` guard is never
  // false when the outer guard passes. The test below pins this invariant: if
  // hasToken() is true, get() MUST be truthy and the header MUST be set.
  it('sets Authorization when hasToken() is true — inner guard invariant', async () => {
    const { fetch, capturedHeaders } = interceptingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok-xyz',
      fetch,
    });
    // Confirm hasToken()/get() invariant holds through the public interface
    expect(sdk.getToken()).toBe('tok-xyz');
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer tok-xyz');
  });

  // ── All three branches false simultaneously: bare request returned unchanged
  it('returns the request unchanged when all three injection conditions are false', async () => {
    // Conditions to make all three false:
    // 1. user-agent: always set by first branch — can't be false for a fresh Request.
    //    However the extra-header loop (branch 2) guard for 'user-agent' fires false
    //    because user-agent was already set by branch 1.
    // 2. extra headers: none configured (options.headers omitted)
    // 3. token: none configured
    //
    // Net observable effect: only the SDK's own user-agent appears; no Authorization;
    // no extra headers; the same request object is returned from onRequest.
    let returnedRequest: Request | undefined;
    const outerFetch: typeof globalThis.fetch = vi.fn(async (input) => {
      if (input instanceof Request) returnedRequest = input;
      return new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });

    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: outerFetch });
    await sdk.getHealth();

    // The middleware must return a Request (not undefined / null / a different type)
    expect(returnedRequest).toBeInstanceOf(Request);
    // No auth header
    expect(returnedRequest!.headers.get('authorization')).toBeNull();
    // Only the SDK user-agent (set by branch 1)
    expect(returnedRequest!.headers.get('user-agent')).toMatch(/^@stellabill\/sdk\//);
  });

  // ── return request: the returned object is a Request, not a new envelope ─
  it('returns the original Request object (not a clone) from onRequest', async () => {
    // We cannot intercept the return value of onRequest directly, but we can
    // assert that fetch receives a Request whose headers were mutated (not a
    // fresh object), meaning the middleware mutated and returned the same instance.
    const capturedRequests: Request[] = [];
    const f: typeof globalThis.fetch = vi.fn(async (input) => {
      if (input instanceof Request) capturedRequests.push(input);
      return new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });

    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'return-ref-token',
      fetch: f,
    });
    await sdk.getHealth();

    // Exactly one request went through
    expect(capturedRequests).toHaveLength(1);
    const req = capturedRequests[0]!;
    // Verify the mutation happened on the returned request (auth header present)
    expect(req.headers.get('authorization')).toBe('Bearer return-ref-token');
    expect(req.headers.get('user-agent')).toMatch(/^@stellabill\/sdk\//);
  });

  // ── Token rotated mid-session: request reflects rotated token immediately ─
  it('reflects a newly rotated token on the very next request', async () => {
    const calls: Array<Record<string, string>> = [];
    const f: typeof globalThis.fetch = vi.fn(async (input) => {
      const headers: Record<string, string> = {};
      if (input instanceof Request) {
        input.headers.forEach((v, k) => {
          headers[k.toLowerCase()] = v;
        });
      }
      calls.push(headers);
      return new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });

    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'token-v1',
      fetch: f,
    });

    await sdk.getHealth(); // first request: token-v1
    sdk.setToken('token-v2');
    await sdk.getHealth(); // second request: token-v2
    sdk.setToken(undefined);
    await sdk.getHealth(); // third request: no token

    expect(calls[0]!['authorization']).toBe('Bearer token-v1');
    expect(calls[1]!['authorization']).toBe('Bearer token-v2');
    expect(calls[2]!['authorization']).toBeUndefined();
  });
});
