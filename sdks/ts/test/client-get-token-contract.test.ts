/**
 * Focused contract tests for the `getToken()` accessor
 * (`sdks/ts/src/client.ts:248` — `return tokenHolder.get();`) and for the
 * "accepted input" side of `parsedError` in the result envelope.
 *
 * These complement `test/client.test.ts` (setToken rotation, malformed token
 * dropped from the wire) and `test/auth.test.ts` (sanitizeToken table) by
 * pinning the *observable* contract of the accessor: it must reflect
 * `sanitizeToken()` of the constructor input, and it must be the exact value
 * the auth middleware puts on the `Authorization` header.
 */
import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

type FetchCall = {
  url: string;
  /** Headers observed on the Request object openapi-fetch hands to fetch. */
  headers: Record<string, string>;
};

/** Minimal mock fetch: records URL + headers and always returns `body`/`status`. */
function mockFetch(
  body: unknown,
  init: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, { status, headers: { 'content-type': contentType } });
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), headers });
    return res;
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';

describe('getToken() - constructor input is sanitized', () => {
  it('returns the trimmed token for a constructor token with surrounding whitespace', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '   secret-token   ', fetch });
    // sanitizeToken() trims before the accessor ever exposes it.
    expect(sdk.getToken()).toBe('secret-token');
  });

  it('returns the trimmed token for tab/newline surrounding whitespace', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '\tsecret-token\n', fetch });
    expect(sdk.getToken()).toBe('secret-token');
  });

  it('returns undefined for a malformed token containing inner whitespace', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'not a valid token', fetch });
    expect(sdk.getToken()).toBeUndefined();
  });

  it('returns undefined for a whitespace-only token', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '     ', fetch });
    expect(sdk.getToken()).toBeUndefined();
  });

  it('returns undefined for a non-string token', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({
      baseUrl: BASE,
      token: 12345 as unknown as string,
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();
  });

  it('returns undefined when no token is supplied', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    expect(sdk.getToken()).toBeUndefined();
  });
});

describe('getToken() - setToken() rotation contract', () => {
  it('reflects a sanitized rotated token, then clears on setToken(undefined)', () => {
    const { fetch } = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'initial', fetch });
    expect(sdk.getToken()).toBe('initial');

    sdk.setToken('  rotated-token  ');
    expect(sdk.getToken()).toBe('rotated-token');

    sdk.setToken('still has whitespace');
    expect(sdk.getToken()).toBeUndefined();

    sdk.setToken('final');
    expect(sdk.getToken()).toBe('final');

    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
  });
});

describe('getToken() - is the exact Authorization value on the wire (:248 branch)', () => {
  it('sends `Bearer ${getToken()}` for a sanitized constructor token', async () => {
    const { fetch, calls } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '  wire-token  ', fetch });

    await sdk.getHealth();

    expect(sdk.getToken()).toBe('wire-token');
    expect(calls).toHaveLength(1);
    expect(calls[0]!.headers['authorization']).toBe(`Bearer ${sdk.getToken()}`);
  });

  it('sends `Bearer ${getToken()}` after setToken rotation', async () => {
    const first = mockFetch({});
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'old', fetch: first.fetch });
    sdk.setToken('  fresh-token  ');

    const second = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const rotated = createStellarBillClient({ baseUrl: BASE, token: sdk.getToken(), fetch: second.fetch });
    await rotated.getHealth();

    expect(rotated.getToken()).toBe('fresh-token');
    expect(second.calls[0]!.headers['authorization']).toBe(`Bearer ${rotated.getToken()}`);
  });

  it('omits Authorization when the constructor token is malformed (getToken() undefined)', async () => {
    const { fetch, calls } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'bad token', fetch });

    await sdk.getHealth();

    expect(sdk.getToken()).toBeUndefined();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('omits Authorization after setToken(undefined) while getToken() is undefined', async () => {
    const { fetch, calls } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'live', fetch });
    sdk.setToken(undefined);

    await sdk.getHealth();

    expect(sdk.getToken()).toBeUndefined();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });
});

describe('parsedError - accepted (2xx) input keeps error undefined', () => {
  it('2xx success returns data and leaves result.error undefined', async () => {
    const { fetch, calls } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(calls).toHaveLength(1);
    expect(r.status).toBe(200);
    expect(r.response.ok).toBe(true);
    expect(r.data?.status).toBe('ok');
    // parsedError is only populated from openapi-fetch's `error` on non-2xx.
    expect(r.error).toBeUndefined();
  });

  it('2xx success on a list endpoint returns data and leaves result.error undefined', async () => {
    const { fetch } = mockFetch({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.listSubscriptions();

    expect(r.status).toBe(200);
    expect(r.data?.subscriptions).toEqual([]);
    expect(r.error).toBeUndefined();
  });

  it('a 2xx body that merely LOOKS like an error body is data, not parsedError', async () => {
    // Boundary: an `error`-shaped key in a successful payload must not leak
    // into result.error; parsedError reflects only the HTTP-level error slot.
    const { fetch } = mockFetch({
      status: 'ok',
      service: 'stellarbill-backend',
      error: 'looks-like-an-error',
      message: 'also looks like one',
    });
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const r = await sdk.getHealth();

    expect(r.status).toBe(200);
    expect((r.data as unknown as Record<string, unknown>)['error']).toBe('looks-like-an-error');
    expect(r.error).toBeUndefined();
  });
});
