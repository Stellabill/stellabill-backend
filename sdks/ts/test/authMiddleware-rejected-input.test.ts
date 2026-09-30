
import { describe, expect, it, vi } from 'vitest';
import { createStellarBillClient } from '../src/index.js';

describe('authMiddleware - rejected input', () => {
  it('should reject invalid headers and not inject them', async () => {
    const calls: any[] = [];
    const mockFetch = vi.fn(async (input) => {
      const headers = new Headers((input as Request).headers);
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
        'x-number': 123 as any,
        'x-object': {} as any,
        'x-array': [] as any,
      },
      fetch: mockFetch,
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
});

