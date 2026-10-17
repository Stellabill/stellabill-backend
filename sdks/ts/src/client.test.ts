import { describe, it, expect, vi } from 'vitest';

import { createStellarBillClient, normalizeErrorBody, isLocalhost } from './client.js';

import type { FetchLike } from './client.js';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function rawResponse(body: string, status = 200, contentType = 'application/json'): Response {
  return new Response(body, {
    status,
    headers: contentType ? { 'content-type': contentType } : {},
  });
}

function makeFetch(handler: (input: RequestInfo, init?: RequestInit) => Promise<Response>): FetchLike {
  return vi.fn((input: RequestInfo, init?: RequestInit) => handler(input, init));
}

describe('normalizeErrorBody', () => {
  it('returns plain objects as the error body', () => {
    const body = { message: 'boom', code: 'ERR' };
    expect(normalizeErrorBody(body)).toEqual(body);
  });

  it('returns undefined for null, arrays, and primitives', () => {
    expect(normalizeErrorBody(null)).toBeUndefined();
    expect(normalizeErrorBody(undefined)).toBeUndefined();
    expect(normalizeErrorBody([])).toBeUndefined();
    expect(normalizeErrorBody('string')).toBeUndefined();
    expect(normalizeErrorBody(42)).toBeUndefined();
  });
});

describe('isLocalhost', () => {
  it('detects localhost and 127.0.0.1', () => {
    expect(isLocalhost('http://localhost:3000')).toBeTrue();
    expect(isLocalhost('http://127.0.0.1:8080')).toBeTrue();
  });

  it('returns false for non-localhost and malformed URLs', () => {
    expect(isLocalhost('https://api.example.org')).toBe(false);
    expect(isLocalhost('not a url')).toBe(false);
  });
});

describe('createStellarBillClient configuration', () => {
  it('throws when baseUrl is missing', () => {
    expect(() =>
      createStellarBillClient({ baseUrl: undefined as unknown as string }),
    ).toThrow(/baseUrl is required/);
  });

  it('throws when baseUrl is blank', () => {
    expect(() => createStellarBillClient({ baseUrl: '   ' })).toThrow(/non-empty string/);
  });

  it('throws when baseUrl is malformed', () => {
    expect(() => createStellarBillClient({ baseUrl: 'not a url' })).toThrow(/not a valid URL/);
  });

  it('throws when no fetch implementation is available', () => {
    const original = globalThis.fetch;
    // @ts-expect-error
    globalThis.fetch = undefined;
    try {
      expect(() => createStellarBillClient({ baseUrl: 'https://api.stellabill.com' })).toThrow(
        /No fetch implementation/,
      );
    } finally {
      globalThis.fetch = original;
    }
  });

  it('strips trailing slashes from baseUrl', async () => {
    const fetch = makeFetch(async () => jsonResponse({ status: 'ok' }));
    const sdk = createStellarBillClient({ baseUrl: 'https://api.stellabill.com///', fetch });
    await sdk.getHealth();
    const call = (fetch as unknown as { mock: { calls: [unknown, RequestInit][] } }).mock.calls[0];
    const url = typeof call[0] === 'string' ? call[0] : (call[0] as Request).url;
    expect(url.startsWith('https://api.stellabill.com/api/health')).toBeTrue();
  });
});

describe('safeParseErrorBody via public wrappers', () => {
  const baseUrl = 'https://api.stellabill.com';

  it('returns undefined error when the error body text is empty', async () => {
    const fetch = makeFetch(async () => rawResponse('', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when the body is not JSON', async () => {
    const fetch = makeFetch(async () => rawResponse('not json', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when content-type is not JSON', async () => {
    const fetch = makeFetch(async () => rawResponse('{}', 500, 'text/plain'));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when the body is whitespace-only', async () => {
    const fetch = makeFetch(async () => rawResponse('   \n', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when the JSON body is null', async () => {
    const fetch = makeFetch(async () => rawResponse('null', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when the JSON body is an array', async () => {
    const fetch = makeFetch(async () => rawResponse('[]', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });

  it('returns undefined error when the JSON body is a primitive', async () => {
    const fetch = makeFetch(async () => rawResponse('42', 500));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toBeUndefined();
  });
});

describe('StellarBillClient operations', () => {
  const baseUrl = 'https://api.stellabill.com';

  it('getHealth returns data and request metadata', async () => {
    const fetch = makeFetch(async () => jsonResponse({ status: 'ok' }));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(200);
    expect(result.data).toEqual({ status: 'ok' });
    expect(result.error).toBeUndefined();
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
  });

  it('getSubscription rejects an empty id', async () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    await expect(sdk.getSubscription('')).rejects.toThrow(
      /subscription id must be a non-empty string/,
    );
    expect(fetch).not.toHaveBeenCalled();
  });

  it('getSubscription rejects a non-string id', async () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    await expect(sdk.getSubscription(123 as unknown as string)).rejects.toThrow(
      /subscription id must be a non-empty string/,
    );
    expect(fetch).not.toHaveBeenCalled();
  });

  it('inspectIdempotencyKey rejects an empty key', async () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    await expect(sdk.inspectIdempotencyKey('')).rejects.toThrow(
      /idempotency key must be a non-empty string/,
    );
    expect(fetch).not.toHaveBeenCalled();
  });

  it('inspectIdempotencyKey rejects a key exceeding 255 characters', async () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    await expect(sdk.inspectIdempotencyKey('a'.repeat(256))).rejects.toThrow(
      /exceeds maximum length of 255/,
    );
    expect(fetch).not.toHaveBeenCalled();
  });

  it('inspectIdempotencyKey accepts a 255-character key', async () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.inspectIdempotencyKey('a'.repeat(255));
    expect(result.status).toBe(200);
    expect(fetch).toHaveBeenCalledOnce();
  });

  it('throwOnError throws StellarBillError with actionable message', async () => {
    const fetch = makeFetch(async () =>
      jsonResponse({ message: 'not found' }, 404),
    );
    const sdk = createStellarBillClient({ baseUrl, fetch, throwOnError: true });
    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 404,
    });
  });

  it('throwOnError is off by default and returns error envelope', async () => {
    const fetch = makeFetch(async () =>
      jsonResponse({ message: 'boom' }, 500),
    );
    const sdk = createStellarBillClient({ baseUrl, fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(500);
    expect(result.error).toEqual({ message: 'boom' });
  });

  it('setToken and getToken round-trip', () => {
    const fetch = makeFetch(async () => jsonResponse({}));
    const sdk = createStellarBillClient({ baseUrl, fetch, token: '  abc  ' });
    expect(sdk.getToken()).toBe('abc');
    sdk.setToken('def');
    expect(sdk.getToken()).toBe('def');
    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
  });

  it('injects authorization and user-agent headers', async () => {
    const fetch = makeFetch(async () => jsonResponse({ status: 'ok' }));
    const sdk = createStellarBillClient({ baseUrl, fetch, token: 'tok' });
    await sdk.getHealth();
    const init = (fetch as unknown as { mock: { calls: [unknown, RequestInit][] } }).mock.calls[0][1];
    const headers = new Headers(init.headers);
    expect(headers.get('authorization')).toBe('Bearer tok');
    expect(headers.get('user-agent')).toBeTruthy();
  });
});
