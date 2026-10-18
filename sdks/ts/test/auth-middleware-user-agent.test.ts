/**
 * Accepted-input coverage for the SDK's `authMiddleware` user-agent branch.
 *
 * `sdks/ts/src/client.ts` guards the default user-agent with:
 *
 * ```ts
 * if (!request.headers.has('user-agent')) {
 *   request.headers.set('user-agent', userAgent);
 * }
 * ```
 *
 * Both sides of that guard are observable from the public client surface:
 *
 *  - the request arrives *without* a user-agent → the SDK default is applied;
 *  - the request arrives *with* one (per-request `headers`) → the caller's
 *    value is accepted and left untouched.
 *
 * These tests pin the accepted input, the resulting request headers, and the
 * fact that the branch never interferes with the Authorization header.
 */

import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, defaultUserAgent, SDK_VERSION } from '../src/index.js';

type ObservedRequest = {
  url: string;
  headers: Record<string, string>;
};

/** Fetch stub that records the headers of every dispatched Request. */
function recordingFetch(body: unknown = { status: 'ok', service: 'stellabill-backend' }) {
  const requests: ObservedRequest[] = [];
  const text = typeof body === 'string' ? body : JSON.stringify(body);
  const fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    requests.push({ url: input instanceof Request ? input.url : String(input), headers });
    return new Response(text, {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, requests };
}

function makeClient(
  fetch: typeof globalThis.fetch,
  options: Partial<Parameters<typeof createStellarBillClient>[0]> = {},
) {
  return createStellarBillClient({
    baseUrl: 'https://api.example.com',
    fetch,
    ...options,
  });
}

describe('authMiddleware - accepted input for the user-agent branch', () => {
  it('accepts a request that carries no user-agent and applies the SDK default', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch);

    await sdk.getHealth();

    expect(requests).toHaveLength(1);
    expect(requests[0]!.headers['user-agent']).toBe(defaultUserAgent());
    expect(requests[0]!.headers['user-agent']).toBe(
      `@stellabill/sdk/${SDK_VERSION} node/${process.versions.node}`,
    );
  });

  it('accepts a caller-supplied per-request user-agent and leaves it untouched', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch);

    await sdk.raw.GET('/api/health', { headers: { 'user-agent': 'caller-agent/1.0' } });

    expect(requests).toHaveLength(1);
    expect(requests[0]!.headers['user-agent']).toBe('caller-agent/1.0');
    // The guard is case-insensitive: the SDK's value must not be appended.
    expect(requests[0]!.headers['user-agent']).not.toContain('@stellabill/sdk');
  });

  it('matches the user-agent header case-insensitively', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch);

    await sdk.raw.GET('/api/health', { headers: { 'User-Agent': 'mixed-case-agent/2.0' } });

    expect(requests[0]!.headers['user-agent']).toBe('mixed-case-agent/2.0');
  });

  it('keeps the accepted user-agent alongside Authorization and static headers', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch, { token: 'tok-123', headers: { 'X-Tenant-ID': 'tenant-a' } });

    await sdk.raw.GET('/api/health', { headers: { 'user-agent': 'accepted-agent/1.0' } });

    const headers = requests[0]!.headers;
    expect(headers['user-agent']).toBe('accepted-agent/1.0');
    expect(headers['authorization']).toBe('Bearer tok-123');
    expect(headers['x-tenant-id']).toBe('tenant-a');
  });

  it('does not leak a per-request user-agent into subsequent requests', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch);

    await sdk.raw.GET('/api/health', { headers: { 'user-agent': 'one-shot-agent/1.0' } });
    await sdk.getHealth();
    await sdk.raw.GET('/api/health', { headers: { 'user-agent': 'one-shot-agent/1.0' } });

    expect(requests.map((r) => r.headers['user-agent'])).toEqual([
      'one-shot-agent/1.0',
      defaultUserAgent(),
      'one-shot-agent/1.0',
    ]);
  });

  it('sets the SDK user-agent on every typed wrapper request', async () => {
    const { fetch, requests } = recordingFetch({ plans: [], pagination: { has_more: false } });
    const sdk = makeClient(fetch);

    await sdk.getHealth();
    await sdk.listPlans({ limit: 5 });
    await sdk.listSubscriptions({ cursor: 'c' });

    expect(requests).toHaveLength(3);
    for (const request of requests) {
      expect(request.headers['user-agent']).toBe(defaultUserAgent());
    }
  });

  it('emits the user-agent exactly once per request', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch, { headers: { 'X-Trace': 'trace-1' } });

    await sdk.getHealth();

    const keys = Object.keys(requests[0]!.headers);
    expect(keys.filter((k) => k.toLowerCase() === 'user-agent')).toEqual(['user-agent']);
    expect(keys.filter((k) => k.toLowerCase() === 'x-trace')).toEqual(['x-trace']);
  });

  it('prefers the SDK user-agent over a static headers entry, but accepts a per-request one', async () => {
    const { fetch, requests } = recordingFetch();
    const sdk = makeClient(fetch, { headers: { 'User-Agent': 'static-agent/9.9' } });

    await sdk.getHealth();
    // Static headers are normalised into the middleware's static set, which the
    // SDK overwrites with its own identifier.
    expect(requests[0]!.headers['user-agent']).toBe(defaultUserAgent());

    await sdk.raw.GET('/api/health', { headers: { 'user-agent': 'per-request-agent/9.9' } });
    // A per-request header is already on the Request, so the accepted-input
    // branch keeps it.
    expect(requests[1]!.headers['user-agent']).toBe('per-request-agent/9.9');
  });
});
