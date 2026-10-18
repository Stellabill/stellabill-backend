/**
 * Focused coverage for the `SdkResult` envelope assembled in `wrap()`
 * (`sdks/ts/src/client.ts`):
 *
 * ```ts
 * const parsedError: ApiErrorBody | undefined =
 *   error && typeof error === 'object' ? (error as ApiErrorBody) : undefined;
 * ...
 * return { data, error: parsedError, status, response, requestMethod: method, requestUrl: response.url || urlPath };
 * ```
 *
 * Two things there are load-bearing and were previously only exercised
 * incidentally:
 *
 * 1. `response.url || urlPath` — the fallback. `openapi-fetch` hands back the
 *    `Response` produced by the caller's fetch, and a `Response` built in a test
 *    double (or by a fetch shim that does not attach a URL) reports `url === ''`.
 *    Every existing test hits the fallback branch without asserting it.
 * 2. `parsedError` narrowing — a non-2xx body that is not a JSON object (`text/plain`
 *    body, empty body) reaches `wrap()` as a *string* from openapi-fetch, and the
 *    envelope must present `undefined` rather than leaking a string into `error`.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

const BASE_URL = 'https://api.example.com';

type FetchCall = {
  url: string;
  method: string;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

/**
 * A fetch double that returns a *fresh* `Response` per call (bodies are
 * single-use) and records the requested URL + method.
 */
function responseFetch(makeResponse: () => Response): {
  fetch: typeof globalThis.fetch;
  calls: FetchCall[];
} {
  const calls: FetchCall[] = [];
  const fetch = vi.fn(async (input: Parameters<typeof fetch>[0]) => {
    const method = input instanceof Request ? input.method : 'GET';
    calls.push({ url: toUrl(input), method });
    return makeResponse();
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, calls };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function textResponse(body: string, status: number): Response {
  return new Response(body, { status, headers: { 'content-type': 'text/plain' } });
}

/**
 * `Response.url` is a read-only getter with no public setter, so a fetch shim
 * that reports an absolute URL has to shadow it. This stands in for the real
 * undici/browser fetch, which does populate `response.url`.
 */
function withUrl(response: Response, url: string): Response {
  Object.defineProperty(response, 'url', { value: url, configurable: true });
  return response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SdkResult envelope - requestUrl resolution', () => {
  it('falls back to the SDK path when the Response reports an empty url', async () => {
    const { fetch } = responseFetch(() => jsonResponse({ status: 'ok' }));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.requestUrl).toBe('/api/health');
    expect(result.requestUrl).not.toContain(BASE_URL);
  });

  it('uses the absolute response url when the fetch layer reports one', async () => {
    const { fetch } = responseFetch(() =>
      withUrl(jsonResponse({ status: 'ok' }), `${BASE_URL}/api/health`),
    );
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.requestUrl).toBe(`${BASE_URL}/api/health`);
  });

  it('prefers response.url over the SDK path on a non-2xx response too', async () => {
    const { fetch } = responseFetch(() =>
      withUrl(jsonResponse({ message: 'nope' }, 503), `${BASE_URL}/api/v1/plans?limit=1`),
    );
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.listPlans({ limit: 1 });

    expect(result.status).toBe(503);
    expect(result.requestUrl).toBe(`${BASE_URL}/api/v1/plans?limit=1`);
  });

  type EnvelopeCall = (sdk: ReturnType<typeof createStellarBillClient>) => Promise<{
    requestUrl: string;
    requestMethod: string;
  }>;
  const envelopeCases: ReadonlyArray<[string, EnvelopeCall, string, string]> = [
    ['getHealth', (sdk) => sdk.getHealth(), '/api/health', 'GET'],
    ['listPlans', (sdk) => sdk.listPlans(), '/api/v1/plans', 'GET'],
    ['listSubscriptions', (sdk) => sdk.listSubscriptions(), '/api/subscriptions', 'GET'],
    ['getSubscription', (sdk) => sdk.getSubscription('sub/1'), '/api/subscriptions/sub%2F1', 'GET'],
    [
      'inspectIdempotencyKey',
      (sdk) => sdk.inspectIdempotencyKey('key 1'),
      '/api/v1/idempotency/key%201',
      'GET',
    ],
  ];

  it.each(envelopeCases)('%s reports the SDK path and method on the envelope', async (_name, call, path, method) => {
    const { fetch, calls } = responseFetch(() => jsonResponse({ ok: true }));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await call(sdk);

    expect(result.requestUrl).toBe(path);
    expect(result.requestMethod).toBe(method);
    expect(calls).toHaveLength(1);
    // The URL actually requested is the response url (empty here) folded with the baseUrl.
    expect(calls[0]?.url.startsWith(BASE_URL)).toBe(true);
  });

  it('exposes the very same Response instance the fetch layer returned', async () => {
    const only = withUrl(jsonResponse({ status: 'ok' }), `${BASE_URL}/api/health`);
    const { fetch } = responseFetch(() => only);
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.response).toBe(only);
    expect(result.status).toBe(only.status);
  });
});

describe('SdkResult envelope - parsedError narrowing', () => {
  it('keeps the parsed JSON object as the error on a non-2xx response', async () => {
    const payload = { message: 'rate limited', error: 'too_many_requests', retryAfter: 30 };
    const { fetch } = responseFetch(() => jsonResponse(payload, 429));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(429);
    expect(result.error).toEqual(payload);
    expect(result.data).toBeUndefined();
    expect(result.requestUrl).toBe('/api/health');
  });

  it('narrows a non-object (text/plain) error body to undefined', async () => {
    const { fetch } = responseFetch(() => textResponse('upstream unavailable', 502));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(502);
    expect(result.error).toBeUndefined();
    expect(result.data).toBeUndefined();
    // The envelope still carries enough to build an actionable error.
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toBe('/api/health');
  });

  it('narrows an empty error body to undefined', async () => {
    const { fetch } = responseFetch(() => new Response('', { status: 500 }));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('leaves error undefined on a 2xx response', async () => {
    const { fetch } = responseFetch(() => jsonResponse({ status: 'ok', version: '0.2.0' }));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data).toEqual({ status: 'ok', version: '0.2.0' });
  });

  it('reports undefined data and undefined error for 204 No Content', async () => {
    const { fetch } = responseFetch(() => new Response(null, { status: 204 }));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(204);
    expect(result.data).toBeUndefined();
    expect(result.error).toBeUndefined();
    expect(result.requestUrl).toBe('/api/health');
  });

  it('carries the parsed body into the thrown StellarBillError when throwOnError is set', async () => {
    const payload = { message: 'conflict' };
    const { fetch } = responseFetch(() => jsonResponse(payload, 409));
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch, throwOnError: true });

    await expect(sdk.getHealth()).rejects.toBeInstanceOf(StellarBillError);
    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 409,
      requestUrl: '/api/health',
      requestMethod: 'GET',
      body: payload,
    });
  });
});
