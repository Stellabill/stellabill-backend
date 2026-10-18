import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, type StellarBillClient } from '../src/index.js';

/**
 * Regression coverage for the rejected-input path around the `wrap` helper
 * that consumes the raw openapi-fetch result (sdks/ts/src/client.ts:219-238;
 * the `const r = (await rawResult)` binding is at :222).
 *
 * Every typed wrapper funnels through `wrap`, so when the underlying request
 * rejects the wrapper must reject too — never fabricate an `SdkResult` with a
 * missing/invalid `Response`. The envelope also has to stay stable for
 * primitive (non-object) error bodies and for responses whose `url` is empty.
 */

type Wrapper = { name: string; call: (sdk: StellarBillClient) => Promise<unknown> };

const wrappers: Wrapper[] = [
  { name: 'getHealth', call: (sdk) => sdk.getHealth() },
  { name: 'listPlans', call: (sdk) => sdk.listPlans() },
  { name: 'listSubscriptions', call: (sdk) => sdk.listSubscriptions() },
  { name: 'getSubscription', call: (sdk) => sdk.getSubscription('sub-1') },
  { name: 'inspectIdempotencyKey', call: (sdk) => sdk.inspectIdempotencyKey('k1') },
];

function fetchReturning(response: Response): typeof globalThis.fetch {
  return (async () => response) as unknown as typeof globalThis.fetch;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('wrap - rejected input on the awaited rawResult (client.ts:222)', () => {
  it.each(wrappers)('$name rejects when the transport rejects', async ({ call }) => {
    const transport = new TypeError('network down');
    const fetch = (async () => {
      throw transport;
    }) as unknown as typeof globalThis.fetch;
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await expect(call(sdk)).rejects.toBe(transport);
  });

  it('falls back to the declared request path when the Response.url is empty', async () => {
    const res = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    // Constructed responses have an empty url; the SDK must fall back to the
    // declared operation path rather than reporting an empty requestUrl.
    expect(res.url).toBe('');
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchReturning(res),
    });

    const r = await sdk.getHealth();
    expect(r.requestUrl).toBe('/api/health');
  });

  it('prefers the resolved Response.url when it is present', async () => {
    const res = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    Object.defineProperty(res, 'url', {
      configurable: true,
      value: 'https://api.example.com/api/health?trace=1',
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchReturning(res),
    });

    const r = await sdk.getHealth();
    expect(r.requestUrl).toBe('https://api.example.com/api/health?trace=1');
  });

  it('normalizes a primitive (non-object) error body to undefined while keeping the status', async () => {
    // A 4xx whose JSON body is a bare string parses to a primitive; the SDK
    // envelope must not expose it as an `ApiErrorBody`.
    const res = new Response(JSON.stringify('teapot'), {
      status: 418,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchReturning(res),
    });

    const r = await sdk.getHealth();
    expect(r.status).toBe(418);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();
  });

  it('keeps the encoded id in the fallback requestUrl for getSubscription', async () => {
    const res = new Response(JSON.stringify({ id: 'a/b' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: fetchReturning(res),
    });

    const r = await sdk.getSubscription('a/b');
    expect(r.requestUrl).toBe('/api/subscriptions/a%2Fb');
  });
});
