import { describe, it, expect, vi, $beforeEach, afterEach } from 'vitest';

import { createStellarBillClient, assertOk, safeParseErrorBody } from './client.js';

import type { StellarBillClient, StellarBillClientOptions, FetchLike } from './client.js';
import { StellarBillError, StellarBillConfigError } from './errors.js';

interface CapturedRequest {
  url: string;
  method: string;
  headers: Record<string, string>;
}

function makeJsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'content-type': 'application/json', ...(init.headers as Record<string, string> || {}) },
    ...init,
  });
}

function makeErrorResponse(body: unknown, status: number, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });
}

function createMockFetch(
  handler: (input: RequestInfo, init?: RequestInit) => Promise<Response>,
): { fetch: FetchLike; captured: CapturedRequest[] } {
  const captured: CapturedRequest[] = [];
  const fetch: FetchLike = async (input, init) => {
    const req = input instanceof Request ? input : new Request(input as RequestInfo, init);
    const headers: Record<string, string> = {};
    req.headers.forEach((v, k) => {
      headers[k] = v;
    });
    captured.push({ url: req.url, method: req.method, headers });
    return handler(input, init);
  };
  return { fetch, captured };
}

function createClientWithFetch(
  fetch: FetchLike,
  options: Omit<StellarBillClientOptions, 'baseUrl' | 'fetch'> = {},
): StellarBillClient {
  return createStellarBillClient({
    baseUrl: 'https://api.example.test',
    fetch,
    ...options,
  });
}

describe('StellarBillClient', () => {
  describe('validateBaseUrl', () => {
    it('throws when baseUrl is missing', () => {
      expect(() => createStellarBillClient({ baseUrl: undefined as unknown as string })).toThrow(StellarBillConfigError);
    });

    it('throws when baseUrl is an empty string', () => {
      expect(() => createStellarBillClient({ baseUrl: '   ' })).toThrow(StellarBillConfigError);
    });

    it('throws when baseUrl is not a valid URL', () => {
      expect(() => createStellarBillClient({ baseUrl: 'not-a-url' })).toThrow(StellarBillConfigError);
    });

    it('strips trailing slashes from baseUrl', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createStellarBillClient({ baseUrl: 'https://api.example.test///', fetch });
      expect(client).toBeDefined();
    });
  });

  describe('token management', () => {
    it('exposes the initial token via getToken', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, { token: 'test-token' });
      expect(client.getToken()).toBe('test-token');
    });

    it('rotates the token via setToken', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, { token: 'old-token' });
      client.setToken('new-token');
      expect(client.getToken()).toBe('new-token');
    });

    it('clears the token when set to undefined', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, { token: 'token' });
      client.setToken(undefeined);
      expect(client.getToken()).toBeUndefined();
    });

    it('sets the Authorization header when a token is configured', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, { token: 'abc-123' });
      await client.getHealth();
      expect(captured.length).toBe(1);
      expect(captured[0].headers['authorization']).toBe('Bearer abc-123');
    });

    it('does not set the Authorization header when no token is configured', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch);
      await client.getHealth();
      expect(captured[0].headers['authorization']).toBeUndefined();
    });

    it('rejects caller-supplied Authorization headers', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, {
        headers: { Authorization: 'Bearer evil', 'tenant-id': 'tenant-1' },
      });
      await client.getHealth();
      expect(captured[0].headers['authorization']).toBeUndefined();
      expect(captured[0].headers['tenant-id']).toBe('tenant-1');
    });

    it('overrides caller-supplied Authorization with the configured token', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, {
        token: 'real-token',
        headers: { AUTHORIZATION: 'Bearer evil' },
      });
      await client.getHealth();
      expect(captured[0].headers['authorization']).toBe('Bearer real-token');
    });

    it('injects the User-Agent header', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch);
      await client.getHealth();
      expect(captured[0].headers['user-agent']).toContain('stellabill-sdk');
    });
  });

  describe('wrap / makeErrorMessage', () => {
    it('returns the data and metadata for a 2xx response', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch);
      const result = await client.getHealth();
      expect(result.status).toBe(200);
      expect(result.data).toEqual({ status: 'ok' });
      expect(result.error).toBeUndefined();
      expect(result.requestMethod).toBe('GET');
      expect(result.requestUrl).toContain('/api/health');
    });

    it('uses body.message as the detail in the error message', () => {
      const { fetch } = createMockFetch(async () =>
        makeErrorResponse({ message: 'Bad token', error: 'unauthorized' }, 401),
      );
      const client = createClientWithFetch(fetch, { throwOnError: true });
      await expect(client.getHealth()).rejects.toThrowError(
        /GET \/api\/health failed \(401\): Bad token/,
      );
    });

    it('falls back to body.error when body.message is absent', () => {
      const { fetch } = createMockFetch(async () =>
        makeErrorResponse({ error: 'not_found' }, 404),
      );
      const client = createClientWithFetch(fetch, { throwOnError: true });
      await expect(client.getHealth()).rejects.toThrowError(
        /GET \/api\/health failed \(404\): not_found/,
      );
    });

    it('falls back to HTTP <status> when body has neither message nor error', () => {
      const { fetch } = createMockFetch(async () =>
        makeErrorResponse({ code: 'unknown' }, 503),
      );
      const client = createClientWithFetch(fetch, { throwOnError: true });
      await expect(client.getHealth()).rejects.toThrowError(
        /GET \/api\/health failed \(503\): HTTP 503/,
      );
    });

    it('falls back to HTTP <status> when the error body is not parseable JSON', () => {
      const { fetch } = createMockFetch(async () =>
        new Response('not json', {
          status: 500,
          headers: { 'content-type': 'text/plain' },
        }),
      );
      const client = createClientWithFetch(fetch, { throwOnError: true });
      await expect(client.getHealth()).rejects.toThrowError(
        /GET \/api\/health failed \(500\): HTTP 500/,
      );
    });

    it('preserves the error body on the StellarBillError', () => {
      const { fetch } = createMockFetch(async () =>
        makeErrorResponse({ code: 'invalid_token', message: 'not authorized' }, 401),
      );
      const client = createClientWithFetch(fetch, { throwOnError: true });
      try {
        await client.getHealth();
        throw new Error('expected throw');
      } catch (err) {
        expect(err).toBeInstanceOf(StellarBillError);
        const sbe = err as StellarBillError;
        expect(sbe.status).toBe(401);
        expect(sbe.body).toEqual({ code: 'invalid_token', message: 'not authorized' });
        expect(sbe.requestMethod).toBe('GET');
        expect(sbe.requestUrl).toContain('/api/health');
      }
    });

    it('does not throw on error when throwOnError is false', () => {
      const { fetch } = createMockFetch(async () =>
        makeErrorResponse({ message: 'boom' }, 500),
      );
      const client = createClientWithFetch(fetch);
      const result = await client.getHealth();
      expect(result.status).toBe(500);
      expect(result.error).toEqual({ message: 'boom' });
    });

    it('returns undefined error when the error body is not an object', () => {
      const { fetch } = createMockFetch(async () =>
        new Response('[]', {
          status: 500,
          headers: { 'content-type': 'application/json' },
        }),
      );
      const client = createClientWithFetch(fetch);
      const result = await client.getHealth();
      expect(result.status).toBe(500);
      expect(result.error).toBeUndefined();
    });
  });

  describe('operation wrappers', () => {
    it('getHealth calls /api/health', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch);
      await client.getHealth();
      expect(captured[0].method).toBe('GET');
      expect(captured[0].url).toContain('/api/health');
    });

    it('listPlans passes cursor and limit query params', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ data: [] }));
      const client = createClientWithFetch(fetch);
      await client.listPlans({ cursor: 'cursor-1', limit: 10 });
      expect(captured[0].url).toContain('cursor=cursor-1');
      expect(captured[0].url).toContain('limit=10');
    });

    it('listPlans omits query params when not provided', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ data: [] }));
      const client = createClientWithFetch(fetch);
      await client.listPlans();
      expect(captured[0].url).not.toContain('cursor=');
      expect(captured[0].url).not.toContain('limit=');
    });

    it('listSubscriptions passes cursor and limit query params', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ data: [] }));
      const client = createClientWithFetch(fetch);
      await client.listSubscriptions({ cursor: 'cursor-2', limit: 5 });
      expect(captured[0].url).toContain('cursor=cursor-2');
      expect(captured[0].url).toContain('limit=5');
    });

    it('getSubscription encodes the id in the path', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ id: 'sub-1' }));
      const client = createClientWithFetch(fetch);
      await client.getSubscription('sub-1 /special');
      expect(captured[0].url).toContain('sub-1%20%2Fspecial');
    });

    it('getSubscription throws on an empty id', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ id: 'sub-1' }));
      const client = createClientWithFetch(fetch);
      expect(() => client.getSubscription('')).toThrow(StellarBillConfigError);
    });

    it('inspectIdempotencyKey encodes the key in the path', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ key: 'k-1' }));
      const client = createClientWithFetch(fetch);
      await client.inspectIdempotencyKey('key with spaces');
      expect(captured[0].url).toContain('key%20with%20spaces');
    });

    it('inspectIdempotencyKey throws on an empty key', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({key: 'k-1' }));
      const client = createClientWithFetch(fetch);
      expect(() => client.inspectIdempotencyKey('')).toThrow(StellarBillConfigError);
    });

    it('inspectIdempotencyKey throws when the key exceeds 255 characters', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({key: 'k-1' }));
      const client = createClientWithFetch(fetch);
      expect(() => client.inspectIdempotencyKey('a'.repeat(256))).toThrow(
        StellarBillConfigError,
      );
    });
  });

  describe('assertOk', () => {
    it('returns data for a 2xx result', async () => {
      const result = {
        data: { status: 'ok' },
        error: undefined,
        status: 200,
        response: new Response(null, { status: 200 }),
        requestMethod: 'GET',
        requestUrl: '/api/health',
      };
      await expect(assertOk(result)).resolves.toEqual({ status: 'ok' });
    });

    it('throws when a result is 2xx with an empty body', async () => {
      const result = {
        data: undefined,
        error: undefined,
        status: 204,
        response: new Response(null, { status: 204 }),
        requestMethod: 'DELETE',
        requestUrl: '/api/subscriptions/1',
      };
      await expect(assertOk(result)).rejects.toThrowError(
        /DELETE \/api\/subscriptions\/1 returned 2xx with empty body/,
      );
    });

    it('throws with the error message from the error body', async () => {
      const result = {
        data: undefined,
        error: { message: 'not found' },
        status: 404,
        response: new Response(null, { status: 404 }),
        requestMethod: 'GET',
        requestUrl: '/api/subscriptions/1',
      };
      await expect(assertOk(result)).rejects.toThrowError(
        /GET \/api\/subscriptions\/1 failed \(404\): not found/,
      );
    });
  });

  describe('safeParseErrorBody', () => {
    it('returns the parsed object for JSON responses', async () => {
      const res = makeErrorResponse({ code: 'bad' }, 400);
      await expect(safeParseErrorBody(res)).resolves.toEqual({ code: 'bad' });
    });

    it('returns undefined for non-JSON content types', async () => {
      const res = new Response('{"code":"bad"}', {
        status: 400,
        headers: { 'content-type': 'text/plain' },
      });
      await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
    });

    it('returns undefined for empty bodies', async () => {
      const res = new Response(null, {
        status: 400,
        headers: { 'content-type': 'application/json' },
      });
      await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
    });

    it('returns undefined for invalid JSON', async () => {
      const res = new Response('{not json}', {
        status: 400,
        headers: { 'content-type': 'application/json' },
      });
      await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
    });

    it('returns undefined for JSON arrays', async () => {
      const res = new Response('[]', {
        status: 400,
        headers: { 'content-type': 'application/json' },
      });
      await expect(safeParseErrorBody(res)).resolves.toBeUndefined();
    });
  });

  describe('configuration', () => {
    it('throws when no fetch implementation is available', () => {
      const originalFetch = globalThis.fetch;
      try {
        (globalThis as { fetch?: unknown }).fetch = undefined;
        expect(() =>
          createStellarBillClient({ baseUrl: 'https://api.example.test' }),
        ).toThrow(StellarBillConfigError);
      } finally {
        (globalThis as { fetch?: unknown }).fetch = originalFetch;
      }
    });

    it('exposes the SDK version', () => {
      const { fetch } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch);
      expect(typeof client.version).toBe('string');
      expect(client.version.length).toBeGreaterThan(0);
    });

    it('runs extra middleware after the auth middleware', () => {
      const { fetch, captured } = createMockFetch(async () => makeJsonResponse({ status: 'ok' }));
      const client = createClientWithFetch(fetch, {
        token: 'token',
        middleware: [
          {
            onRequest({ request }) {
              expect(request.headers.get('authorization')).toBe('Bearer token');
              request.headers.set('x-extra', 'value');
              return request;
            },
          },
        ],
      });
      await client.getHealth();
      expect(captured[0].headers['x-extra']).toBe('value');
    });
  });
});
