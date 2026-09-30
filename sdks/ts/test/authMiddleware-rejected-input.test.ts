
import { describe, expect, it, vi } from 'vitest';
import { createStellarBillClient } from '../src/index.js';

describe('authMiddleware - rejected input', () => {
  it('should reject invalid headers and not inject them', async () => {
    const calls: Array<{ headers: Record<string, string> }> = [];
    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      let headers: Headers;
      if (input instanceof Request) {
        headers = input.headers;
      } else {
        headers = new Headers(init?.headers);
      }
      const h: Record<string, string> = {};
      headers.forEach((v, k) => (h[k] = v));
      calls.push({ headers: h });
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200, headers: { 'content-type': 'application/json' }});
    });

    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'x-valid': 'valid-value',
        'x-empty': '',
        'x-whitespace': '   ',
        'x-number': 123 as unknown as string,
        'x-object': {} as unknown as string,
        'x-array': [] as unknown as string,
      },
      fetch: mockFetch as typeof fetch,
    });

    await sdk.getHealth();
    expect(calls.length).toBe(1);
    const h = calls[0].headers;
    expect(h['x-valid']).toBe('valid-value');
    expect(h['x-empty']).toBeUndefined();
    expect(h['x-whitespace']).toBeUndefined();
    expect(h['x-number']).toBeUndefined();
    expect(h['x-object']).toBeUndefined();
    expect(h['x-array']).toBeUndefined();
  });

  it('should not inject authorization header when token is rejected', async () => {
    const calls: Array<{ headers: Record<string, string> }> = [];
    const mockFetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      let headers: Headers;
      if (input instanceof Request) {
        headers = input.headers;
      } else {
        headers = new Headers(init?.headers);
      }
      const h: Record<string, string> = {};
      headers.forEach((v, k) => (h[k] = v));
      calls.push({ headers: h });
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200, headers: { 'content-type': 'application/json' }});
    });

    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'invalid token', // Rejected by sanitizeToken due to whitespace
      fetch: mockFetch as typeof fetch,
    });

    await sdk.getHealth();
    expect(calls.length).toBe(1);
    expect(calls[0].headers['authorization']).toBeUndefined();
    expect(sdk.getToken()).toBeUndefined();
  });
});

