import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

const health = { status: 'ok', service: 'stellabill-backend' };

function transport(status = 200, body: unknown = health) {
  return vi.fn(async (_input: RequestInfo) => new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  }));
}

describe('caller header value lower boundary (#934)', () => {
  it.each([
    { value: '', present: false, expected: null },
    { value: 'a', present: true, expected: 'a' },
    { value: '0', present: true, expected: '0' },
    // Nonempty whitespace passes the SDK guard; Fetch trims the value.
    { value: ' ', present: true, expected: '' },
  ])('sends the expected Request for value "$value"', async ({ value, present, expected }) => {
    const fetch = transport();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com', fetch,
      headers: { 'X-Boundary': value },
    });

    const result = await sdk.getHealth();

    expect(fetch).toHaveBeenCalledTimes(1);
    const request = fetch.mock.calls[0]![0] as Request;
    expect(request.url).toBe('https://api.example.com/api/health');
    expect(request.headers.has('x-boundary')).toBe(present);
    expect(request.headers.get('x-boundary')).toBe(expected);
    expect(result).toMatchObject({ status: 200, data: health, error: undefined });
  });

  it.each(['', undefined, null, 0, false, {}, ['a']])(
    'ignores invalid/empty value %j without erasing an earlier normalized header', async (value) => {
      const fetch = transport();
      const sdk = createStellarBillClient({
        baseUrl: 'https://api.example.com', fetch, token: 'sdk-token',
        // Deliberately exercise runtime inputs from JavaScript callers.
        headers: {
          'X-Boundary': 'a', 'x-boundary': value, Authorization: 'caller-token',
        } as Record<string, string>,
      });

      const result = await sdk.getHealth();

      expect(fetch).toHaveBeenCalledTimes(1);
      const request = fetch.mock.calls[0]![0] as Request;
      expect(request.headers.get('x-boundary')).toBe('a');
      expect(request.headers.get('authorization')).toBe('Bearer sdk-token');
      expect(result).toMatchObject({ status: 200, data: health, error: undefined });
    },
  );

  it.each([false, true])('preserves HTTP failure semantics with throwOnError=%s', async (throwOnError) => {
    const body = { code: 'forbidden', message: 'Access denied' };
    const fetch = transport(403, body);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com', fetch, throwOnError,
      headers: { 'X-Empty': '', 'X-Boundary': 'a' },
    });

    if (throwOnError) {
      await expect(sdk.getHealth()).rejects.toMatchObject({
        name: StellarBillError.name, status: 403, body,
        requestMethod: 'GET', requestUrl: '/api/health',
        message: 'GET /api/health failed (403): Access denied',
      });
    } else {
      const result = await sdk.getHealth();
      expect(result).toMatchObject({ status: 403, data: undefined, error: body });
      expect(result.response.status).toBe(403);
    }
    expect(fetch).toHaveBeenCalledTimes(1);
    const request = fetch.mock.calls[0]![0] as Request;
    expect(request.headers.has('x-empty')).toBe(false);
    expect(request.headers.get('x-boundary')).toBe('a');
  });
});
