/**
 * Focused coverage for the request-header guards in the SDK's auth middleware
 * (`sdks/ts/src/client.ts`):
 *
 * ```ts
 * if (!request.headers.has('user-agent')) {
 *   request.headers.set('user-agent', userAgent);
 * }
 * for (const [k, v] of Object.entries(extraHeaders)) {
 *   if (!request.headers.has(k) && typeof v === 'string' && v.length > 0) {
 *     request.headers.set(k, v);
 *   }
 * }
 * ```
 *
 * The existing suite asserts *that* a user-agent is attached. It does not pin
 * down the boundary the `has()` guard creates, which is what actually decides
 * the outcome when more than one layer supplies the same header:
 *
 * * the SDK's user-agent wins over one configured through `options.headers`
 *   (caller configuration must not be able to spoof the SDK identity), while
 * * a user-agent already present on the `Request` — i.e. supplied per request —
 *   is left untouched.
 *
 * The same guard is then checked for static `options.headers`: a per-request
 * header of the same name takes priority, and the SDK's `Authorization` still
 * cannot be displaced.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, defaultUserAgent, SDK_VERSION } from '../src/index.js';

const BASE_URL = 'https://api.example.com';
const SDK_UA = defaultUserAgent();

type FetchCall = {
  url: string;
  method: string;
  headers: Record<string, string>;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

/** Records the headers openapi-fetch actually put on the outgoing request. */
function headerRecordingFetch(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch = vi.fn(async (input: Parameters<typeof fetch>[0]) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      method: input instanceof Request ? input.method : 'GET',
      headers,
    });
    return new Response(JSON.stringify({ status: 'ok' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, calls };
}

type RawGetInit = { headers?: Record<string, string> };
type RawGetResult = { data?: unknown; error?: unknown; response: Response };
type RawGet = (url: string, init?: RawGetInit) => Promise<RawGetResult>;

/**
 * openapi-fetch's typed `GET` is parameterised by the generated `paths` map,
 * which this suite deliberately does not depend on; the per-request `headers`
 * option itself is part of openapi-fetch's public `FetchOptions`.
 */
function rawGet(sdk: ReturnType<typeof createStellarBillClient>): RawGet {
  return (sdk.raw as unknown as { GET: RawGet }).GET.bind(sdk.raw);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('user-agent guard', () => {
  it('sets the default SDK user-agent when the request carries none', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    await sdk.getHealth();

    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers['user-agent']).toBe(SDK_UA);
    expect(calls[0]?.headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(calls[0]?.headers['user-agent']).toContain(SDK_VERSION);
  });

  it('encodes exactly one user-agent, so it is never duplicated or merged', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch,
      headers: { 'User-Agent': 'spoofed/1.0', 'user-agent': 'spoofed/2.0' },
    });

    await sdk.getHealth();

    const ua = calls[0]?.headers['user-agent'] ?? '';
    expect(ua).toBe(SDK_UA);
    // Headers joins repeated values with ", " — a merged header would show up here.
    expect(ua).not.toContain(',');
    expect(ua).not.toContain('spoofed');
  });

  it.each([
    ['title-case', 'User-Agent'],
    ['lower-case', 'user-agent'],
    ['upper-case', 'USER-AGENT'],
  ])('replaces a %s header configured through options.headers', async (_label, headerName) => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch,
      headers: { [headerName]: 'spoofed/1.0' },
    });

    await sdk.getHealth();

    expect(calls[0]?.headers['user-agent']).toBe(SDK_UA);
  });

  it('ignores an empty user-agent configured through options.headers', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch,
      headers: { 'User-Agent': '' },
    });

    await sdk.getHealth();

    expect(calls[0]?.headers['user-agent']).toBe(SDK_UA);
  });

  it('leaves a user-agent already present on the request untouched', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await rawGet(sdk)('/api/health', {
      headers: { 'User-Agent': 'custom-agent/9' },
    });

    expect(result.response.status).toBe(200);
    expect(calls[0]?.headers['user-agent']).toBe('custom-agent/9');
  });

  it('gives a per-request header priority over the same options.headers entry', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch,
      headers: { 'x-trace-id': 'configured-value' },
    });

    await rawGet(sdk)('/api/health', { headers: { 'X-Trace-Id': 'per-request-value' } });

    expect(calls[0]?.headers['x-trace-id']).toBe('per-request-value');
  });

  it('still overwrites a per-request Authorization header with the SDK token', async () => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch,
      token: 'sk_live_abc123',
      headers: { 'x-trace-id': 'configured-value' },
    });

    await rawGet(sdk)('/api/health', { headers: { Authorization: 'Bearer smuggled' } });

    expect(calls[0]?.headers['authorization']).toBe('Bearer sk_live_abc123');
  });

  type WrapperCall = (sdk: ReturnType<typeof createStellarBillClient>) => Promise<unknown>;
  const wrapperCalls: ReadonlyArray<[string, WrapperCall]> = [
    ['getHealth', (sdk) => sdk.getHealth()],
    ['listPlans', (sdk) => sdk.listPlans()],
    ['listSubscriptions', (sdk) => sdk.listSubscriptions()],
    ['getSubscription', (sdk) => sdk.getSubscription('sub_1')],
    ['inspectIdempotencyKey', (sdk) => sdk.inspectIdempotencyKey('key_1')],
  ];

  it.each(wrapperCalls)('%s sends the default SDK user-agent', async (_name, call) => {
    const { fetch, calls } = headerRecordingFetch();
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    await call(sdk);

    expect(calls).toHaveLength(1);
    expect(calls[0]?.headers['user-agent']).toBe(SDK_UA);
  });
});
