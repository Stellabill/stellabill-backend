import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillError } from '../src/index.js';

const BASE = 'https://api.example.com';
const PATH = '/api/health';
const HEALTH = { status: 'ok', service: 'stellabill-backend' };

function response(status: number): Response {
  const res = new Response(null, { status });
  Object.defineProperty(res, 'url', { value: `${BASE}${PATH}` });
  return res;
}

describe('getHealth raw.GET result boundary', () => {
  it('passes the exact health request and preserves an accepted raw result', async () => {
    const res = new Response(JSON.stringify(HEALTH), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    Object.defineProperty(res, 'url', { value: `${BASE}${PATH}` });
    const requests: Request[] = [];
    const fetch = vi.fn(async (input: RequestInfo, init?: RequestInit) => {
      requests.push(input instanceof Request ? input : new Request(input, init));
      return res;
    });
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'health-token', fetch });
    const get = vi.spyOn(sdk.raw, 'GET');

    const result = await sdk.getHealth();

    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(requests).toHaveLength(1);
    expect(requests[0]!.method).toBe('GET');
    expect(requests[0]!.url).toBe(`${BASE}${PATH}`);
    expect(requests[0]!.headers.get('authorization')).toBe('Bearer health-token');
    expect(result.data).toEqual(HEALTH);
    expect(result.error).toBeUndefined();
    expect(result.response).toBe(res);
    expect(result).toMatchObject({ status: 200, requestMethod: 'GET', requestUrl: `${BASE}${PATH}` });
    expect(sdk.getToken()).toBe('health-token');
  });

  it('preserves an accepted empty 204 result without fabricating data or an error', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true });
    const res = response(204);
    const get = vi.spyOn(sdk.raw, 'GET').mockResolvedValue({
      data: undefined,
      error: undefined,
      response: res,
    } as never);

    const result = await sdk.getHealth();

    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});
    expect(result).toMatchObject({ status: 204, requestMethod: 'GET', requestUrl: `${BASE}${PATH}` });
    expect(result.data).toBeUndefined();
    expect(result.error).toBeUndefined();
    expect(result.response).toBe(res);
  });

  it('returns the raw rejection body in the ordinary error envelope', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE });
    const body = { code: 'service_unavailable', message: 'temporarily unavailable' };
    const res = response(503);
    const get = vi.spyOn(sdk.raw, 'GET').mockResolvedValue({ data: undefined, error: body, response: res } as never);

    const result = await sdk.getHealth();

    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});
    expect(result.data).toBeUndefined();
    expect(result.error).toBe(body);
    expect(result.status).toBe(503);
    expect(result.response).toBe(res);
  });

  it('throws a typed error for the same raw rejection in strict mode', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE, throwOnError: true });
    const body = { code: 'service_unavailable', message: 'temporarily unavailable' };
    const get = vi.spyOn(sdk.raw, 'GET').mockResolvedValue({ data: undefined, error: body, response: response(503) } as never);

    const error = await sdk.getHealth().catch((reason: unknown) => reason);

    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});
    expect(error).toBeInstanceOf(StellarBillError);
    expect(error).toMatchObject({ status: 503, body, requestMethod: 'GET', requestUrl: PATH });
  });

  it('propagates a rejected raw request unchanged rather than fabricating a response', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE });
    const failure = new Error('network unavailable');
    const get = vi.spyOn(sdk.raw, 'GET').mockRejectedValue(failure);

    await expect(sdk.getHealth()).rejects.toBe(failure);
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});
  });

  it('waits for a delayed raw result and wraps it exactly once', async () => {
    const sdk = createStellarBillClient({ baseUrl: BASE });
    let settle!: (value: unknown) => void;
    const pending = new Promise((resolve) => { settle = resolve; });
    const get = vi.spyOn(sdk.raw, 'GET').mockReturnValue(pending as never);
    let completed = false;

    const resultPromise = sdk.getHealth().then((result) => {
      completed = true;
      return result;
    });
    await Promise.resolve();
    expect(completed).toBe(false);
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(PATH, {});

    settle({ data: HEALTH, error: undefined, response: response(200) });
    const result = await resultPromise;
    expect(completed).toBe(true);
    expect(result.data).toBe(HEALTH);
    expect(get).toHaveBeenCalledTimes(1);
  });
});
