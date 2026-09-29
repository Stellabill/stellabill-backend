import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

const BASE = 'https://api.example.com';
const TOKEN = 'health-test-token';
const HEALTH = { status: 'ok', service: 'stellabill-backend' };
const UNAVAILABLE = { code: 'service_unavailable', message: 'temporarily unavailable' };

function fixture(body: unknown, status = 200, throwOnError = false) {
  const response = new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', 'x-request-id': 'health-test' },
  });
  Object.defineProperty(response, 'url', { value: `${BASE}/api/health` });
  const requests: Request[] = [];
  const fetch = vi.fn(async (input: RequestInfo, init?: RequestInit) => {
    requests.push(input instanceof Request ? input : new Request(input, init));
    return response;
  });
  const sdk = createStellarBillClient({
    baseUrl: `${BASE}/`,
    token: TOKEN,
    headers: { 'X-Tenant-ID': 'tenant-health-test' },
    fetch,
    throwOnError,
  });
  return { sdk, fetch, requests, response };
}

function expectRequest(requests: Request[]) {
  expect(requests).toHaveLength(1);
  const request = requests[0]!;
  expect(request.method).toBe('GET');
  expect(request.url).toBe(`${BASE}/api/health`);
  expect(request.body).toBeNull();
  expect(request.headers.get('authorization')).toBe(`Bearer ${TOKEN}`);
  expect(request.headers.get('x-tenant-id')).toBe('tenant-health-test');
  expect(request.headers.get('user-agent')).toMatch(/^@stellabill\/sdk\//);
}

describe('sdk.getHealth() accepted input contract', () => {
  it.each([false, true])('preserves the documented result and token with throwOnError=%s', async (throwOnError) => {
    const { sdk, fetch, requests, response } = fixture(HEALTH, 200, throwOnError);

    const result = await sdk.getHealth();
    const { data, error } = result;

    expectRequest(requests);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(data).toEqual(HEALTH);
    expect(error).toBeUndefined();
    expect(result).toMatchObject({ status: 200, requestMethod: 'GET', requestUrl: `${BASE}/api/health` });
    expect(result.response).toBe(response);
    expect(result.response.headers.get('x-request-id')).toBe('health-test');
    expect(sdk.getToken()).toBe(TOKEN);
  });

  it('returns an observable error envelope for a valid request rejected by the server', async () => {
    const { sdk, requests, response } = fixture(UNAVAILABLE, 503);

    const result = await sdk.getHealth();

    expectRequest(requests);
    expect(result.data).toBeUndefined();
    expect(result.error).toEqual(UNAVAILABLE);
    expect(result).toMatchObject({ status: 503, requestMethod: 'GET', requestUrl: `${BASE}/api/health` });
    expect(result.response).toBe(response);
    expect(sdk.getToken()).toBe(TOKEN);
  });

  it('throws the documented SDK error for the same server rejection in strict mode', async () => {
    const { sdk, requests } = fixture(UNAVAILABLE, 503, true);

    const error = await sdk.getHealth().catch((reason: unknown) => reason);

    expectRequest(requests);
    expect(error).toBeInstanceOf(StellarBillError);
    expect(error).toMatchObject({
      status: 503,
      body: UNAVAILABLE,
      requestMethod: 'GET',
      requestUrl: '/api/health',
      message: 'GET /api/health failed (503): temporarily unavailable',
    });
    expect(sdk.getToken()).toBe(TOKEN);
  });
});
