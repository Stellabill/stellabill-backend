import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Accepted-input coverage for the `t` branch in the auth middleware
 * (`sdks/ts/src/client.ts:202` — `if (t) request.headers.set(...)`).
 *
 * The issue asks to verify the branch accepts a representative valid input and
 * preserves the documented result or state. We configure a valid token, make a
 * request, and assert the Authorization header is set correctly, preserving
 * the documented HTTP request state and the 200 response result.
 */

type FetchCall = {
  url: string;
  headers: Record<string, string>;
};

function mockFetchOnce(body: unknown): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = JSON.stringify(body);
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), headers });
    return new Response(text, { status: 200, headers: { 'content-type': 'application/json' } });
  });
  return { fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

const BASE = 'https://api.example.com';

describe('auth middleware - t accepted input', () => {
  it('accepts a valid token and preserves the documented authorization state', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    
    // Configure the client with a representative valid token input.
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch, token: 'representative-valid-token' });
    
    // Exercise the branch.
    const result = await sdk.getHealth();
    
    // Verify the documented response state is preserved.
    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data?.status).toBe('ok');
    
    // Assert the exposed request and token behavior.
    expect(calls).toHaveLength(1);
    expect(calls[0]!.headers['authorization']).toBe('Bearer representative-valid-token');
    expect(sdk.getToken()).toBe('representative-valid-token');
  });
});
