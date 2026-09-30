import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  assertOk,
  createStellarBillClient,
  isLocalhost,
  safeParseErrorBody,
  StellarBillConfigError,
  StellarBillError,
} from '../src/index.js';

type FetchCall = {
  url: string;
  init: RequestInit | undefined;
  /** Headers extracted from the Request object (openapi-fetch passes Request as input). */
  inputHeaders: Record<string, string>;
  /** Headers extracted from init.headers (fallback if openapi-fetch passes a URL). */
  initHeaders: Record<string, string>;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

function headersFromInit(init?: RequestInit): Record<string, string> {
  const out: Record<string, string> = {};
  if (!init?.headers) return out;
  new Headers(init.headers).forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

/** Headers as observed by the mock fetch (covers both Request input + URL+init input). */
function callHeaders(call: FetchCall): Record<string, string> {
  return { ...call.initHeaders, ...call.inputHeaders };
}

function mockFetchOnce(
  body: unknown,
  init: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  const text = body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body);
  const res = new Response(text, { status, headers: { 'content-type': contentType } });
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      init: initArg as RequestInit | undefined,
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return res;
  });
  return { fetch, calls };
}

function makeFetchForResponse(response: Response): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      init: initArg as RequestInit | undefined,
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return response;
  });
  return { fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - configuration', () => {
  it.each([
    ['undefined', undefined],
    ['null', null],
  ])('rejects %s baseUrl with a stable config error', (_label, baseUrl) => {
    let thrown: unknown;
    try {
      createStellarBillClient({ baseUrl: baseUrl as unknown as string });
    } catch (error) {
      thrown = error;
    }

    expect(thrown).toBeInstanceOf(StellarBillConfigError);
    expect(thrown).toMatchObject({
      name: 'StellarBillConfigError',
      message: 'baseUrl is required',
    });
  });

  it('throws StellarBillConfigError on malformed baseUrl', () => {
    expect(() => createStellarBillClient({ baseUrl: 123 as unknown as string })).toThrow(
      /non-empty string/
    );
    expect(() => createStellarBillClient({ baseUrl: {} as unknown as string })).toThrow(
      /non-empty string/
    );
    expect(() => createStellarBillClient({ baseUrl: '' })).toThrow(/non-empty/);
    expect(() => createStellarBillClient({ baseUrl: '   ' })).toThrow(/non-empty/);
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url' })).toThrow(/not a valid URL/);
  });

  it('accepts a valid baseUrl and resolves requests against it', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellabill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.data).toEqual({ status: 'ok', service: 'stellabill-backend' });
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('warns when baseUrl is http and not localhost', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
    await sdk.getHealth();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
  });

  it('does not warn when baseUrl is https', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(warn).not.toHaveBeenCalled();
  });

  it('does not warn when baseUrl is http on localhost/127.0.0.1', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch: f1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk1 = createStellarBillClient({ baseUrl: 'http://localhost:8080', fetch: f1 });
    await sdk1.getHealth();
    const { fetch: f2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk2 = createStellarBillClient({ baseUrl: 'http://127.0.0.1:8080', fetch: f2 });
    await sdk2.getHealth();
    expect(warn).not.toHaveBeenCalled();
  });

  it('warns when baseUrl is http with deceptive or unsupported localhost hostnames', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    
    const deceptiveUrls = [
      'http://localhost.example.com',
      'http://127.0.0.1.com',
      'http://mylocalhost',
      'http://[::1]', // IPv6 loopback is not explicitly allowed in isLocalhost
    ];

    for (const url of deceptiveUrls) {
      warn.mockClear();
      const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
      const sdk = createStellarBillClient({ baseUrl: url, fetch });
      await sdk.getHealth();
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
    }
  });

  it('safely handles and rejects invalid input during isLocalhost URL parsing', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    
    const OriginalURL = globalThis.URL;
    try {
      let callCount = 0;
      globalThis.URL = class extends OriginalURL {
        constructor(url: string | URL, base?: string | URL) {
          if (callCount++ === 1) {
            // Throw on the second call (inside isLocalhost)
            throw new TypeError('Simulated invalid URL parsing in isLocalhost');
          }
          super(url, base);
        }
      } as any;

      const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
      // We pass http://localhost so it would normally NOT warn.
      // But because URL throws inside isLocalhost, the catch block returns false,
      // which triggers the warning, giving us a stable observable behavior.
      const sdk = createStellarBillClient({ baseUrl: 'http://localhost', fetch });
      await sdk.getHealth();
      
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
    } finally {
      globalThis.URL = OriginalURL;
    }
  });

  it('throws StellarBillConfigError when no fetch implementation is available', () => {
    const saved = (globalThis as { fetch?: unknown }).fetch;
    (globalThis as { fetch?: unknown }).fetch = undefined;
    try {
      expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com' })).toThrow(/No fetch/);
    } finally {
      (globalThis as { fetch?: unknown }).fetch = saved;
    }
  });

  it('throws StellarBillConfigError when provided fetch is not a function (rejected input)', () => {
    // Pass an explicitly invalid `fetch` value to exercise the rejection branch
    // in createStellarBillClient that checks `typeof providedFetch !== 'function'`.
    // Use a value that might commonly be passed by mistake (string).
    // The API should reject deterministically with a config error.
    // @ts-expect-error: intentionally passing invalid type for test
    expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: 'not-a-function' })).toThrow(
      StellarBillConfigError,
    );
  });

  it('accepts an explicit fetch option', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(calls).toHaveLength(1);
  });

  it('accepts a baseUrl with trailing slashes and resolves requests against it', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('strips trailing slashes from baseUrl including port variants', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://127.0.0.1:8080/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('http://127.0.0.1:8080/api/health');
  });
});

describe('validateBaseUrl - boundary conditions for trailing slash stripping (line 96)', () => {
  it('handles baseUrl with no trailing slash (no-op case)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('handles baseUrl with single trailing slash', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('handles baseUrl with multiple consecutive trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/////', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('preserves path segments and only strips trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/v2/path///', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/v2/path/api/health');
  });

  it('preserves query parameters when stripping trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com?key=value', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toContain('https://api.example.com');
    expect(calls[0]!.url).toContain('key=value');
  });

  it('preserves fragment identifiers when stripping trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/#section', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toContain('https://api.example.com');
  });

  it('handles baseUrl with port and multiple trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com:8443///', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com:8443/api/health');
  });

  it('handles baseUrl with explicit port 443 and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com:443/', fetch });
    await sdk.getHealth();
    // URL.toString() may normalize :443 for https
    expect(calls[0]!.url).toMatch(/^https:\/\/api\.example\.com(:443)?\/api\/health$/);
  });

  it('handles baseUrl with explicit port 80 and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://api.example.com:80/', fetch });
    await sdk.getHealth();
    // URL.toString() may normalize :80 for http
    expect(calls[0]!.url).toMatch(/^http:\/\/api\.example\.com(:80)?\/api\/health$/);
  });

  it('handles baseUrl with subdomain and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.sub.example.com//', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.sub.example.com/api/health');
  });

  it('handles baseUrl with IPv4 address and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://192.168.1.1:8080//', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('http://192.168.1.1:8080/api/health');
  });

  it('handles baseUrl with IPv6 address and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'http://[::1]:8080/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('http://[::1]:8080/api/health');
  });

  it('handles baseUrl with path containing encoded characters and trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/api%20path//', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toContain('/api%20path/api/health');
  });

  it('ensures URL composition does not create double slashes after stripping', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    // Base with trailing slash stripped should compose cleanly with path
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
    expect(calls[0]!.url).not.toContain('//api/health');
  });

  it('ensures consistent behavior across multiple requests after trailing slash removal', async () => {
    // Need separate mocks for each call since Response bodies can only be read once
    const { fetch: fetch1, calls: calls1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const { fetch: fetch2, calls: calls2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    
    const sdk1 = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch: fetch1 });
    const sdk2 = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch: fetch2 });
    
    await sdk1.getHealth();
    await sdk2.getHealth();
    
    expect(calls1[0]!.url).toBe('https://api.example.com/api/health');
    expect(calls2[0]!.url).toBe('https://api.example.com/api/health');
    expect(calls1[0]!.url).toBe(calls2[0]!.url);
  });

  it('handles baseUrl that is root path with trailing slashes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://example.com///', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://example.com/api/health');
  });

  it('validates trailing slash removal does not affect error responses', async () => {
    const { fetch, calls } = mockFetchOnce(
      { error: 'Not Found', message: 'gone', code: 'missing' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch });
    const result = await sdk.getHealth();
    expect(result.status).toBe(404);
    expect(calls[0]!.url).toContain('https://api.example.com');
    expect(calls[0]!.url).not.toMatch(/\/{2,}api\/health/);
  });

  it('validates trailing slash removal does not affect token injection', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com///',
      token: 'test-token',
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBe('Bearer test-token');
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('validates trailing slash removal produces deterministic URLs for caching', async () => {
    const { fetch: fetch1, calls: calls1 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const { fetch: fetch2, calls: calls2 } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    
    const sdk1 = createStellarBillClient({ baseUrl: 'https://api.example.com/', fetch: fetch1 });
    const sdk2 = createStellarBillClient({ baseUrl: 'https://api.example.com///', fetch: fetch2 });
    
    await sdk1.getHealth();
    await sdk2.getHealth();
    
    // Both should produce identical URLs regardless of trailing slash count
    expect(calls1[0]!.url).toBe(calls2[0]!.url);
    expect(calls1[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('handles edge case of baseUrl that is just protocol and domain with trailing slash', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/', fetch });
    await sdk.getHealth();
    expect(calls[0]!.url).toBe('https://api.example.com/api/health');
  });

  it('validates that URL.toString() normalization is preserved after trailing slash removal', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    // URL constructor normalizes certain aspects (e.g., default ports)
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com:443///', fetch });
    await sdk.getHealth();
    // Verify normalization happened and trailing slashes were removed
    expect(calls[0]!.url).toMatch(/^https:\/\/api\.example\.com/);
    expect(calls[0]!.url).toContain('/api/health');
    // Ensure the path portion doesn't have doubled slashes (ignore protocol)
    const urlPath = calls[0]!.url.split('://')[1];
    expect(urlPath).not.toMatch(/\/{2,}/);
  });
});

describe('createStellarBillClient - headers and auth', () => {
  it('accepts an undefined token and omits Authorization from requests', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: undefined,
      fetch,
    });

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();

    expect(callHeaders(calls[0]!)).not.toHaveProperty('authorization');
  });

  it('injects Authorization Bearer header when token is set', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: '  my-token  ',
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBe('Bearer my-token');
  });

  it('rejects trimmed-empty token before attaching Authorization header', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: '  \t\n  ',
      fetch,
    });

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('drops malformed token (whitespace inside)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('drops malformed token (tab inside)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad\ttoken',
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('drops malformed token (newline inside)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad\ntoken',
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('omits Authorization when the token is only whitespace', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: ' \t\n ',
      fetch,
    });

    await sdk.getHealth();

    expect(calls).toHaveLength(1);
    expect(callHeaders(calls[0]!)['authorization']).toBeUndefined();
  });

  it('rejects non-string token input without storing or sending it', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 123 as unknown as string,
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();

    const result = await sdk.getHealth();
    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();

    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('setToken rotates the token; subsequent calls use the new one', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'old', fetch });
    expect(sdk.getToken()).toBe('old');
    sdk.setToken('new');
    expect(sdk.getToken()).toBe('new');
    sdk.setToken('bad token');
    expect(sdk.getToken()).toBeUndefined();
    sdk.setToken(undefined);
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  it('attaches user-agent and static headers on every request', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Custom-Trace': 'abc' },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(headers['x-custom-trace']).toBe('abc');
  });

  it('skips empty-valued static headers', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Empty': '', 'X-Keep': 'v' },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['x-empty']).toBeUndefined();
    expect(headers['x-keep']).toBe('v');
  });

  it('rejects caller-supplied Authorization header (auth bypass prevention)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        Authorization: 'Bearer attacker-controlled-1',
        authorization: 'Bearer attacker-controlled-2',
        AUTHORIZATION: 'Bearer attacker-controlled-3',
      },
      fetch,
    });
    await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });

  // ---- boundary conditions for the header-skip branch (authMiddleware line 196) ----
  // Branch: if (!request.headers.has(k) && typeof v === 'string' && v.length > 0)
  //
  // Condition 1 — skip-if-present (!request.headers.has(k)):
  //   When the Request already carries the header, authMiddleware MUST leave it
  //   unchanged.  The SDK exposes `sdk.raw` so callers can drive openapi-fetch
  //   directly with per-call `headers`; those headers are placed on the Request
  //   object *before* middleware fires, which makes them the "pre-existing" header
  //   that the branch guards against overwriting.

  it('does not overwrite a header that is already present on the Request (skip-if-present)', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      // SDK-level static header — goes into extraHeaders and would be set by authMiddleware
      // if the header is absent from the request.
      headers: { 'x-tenant-id': 'sdk-level-tenant' },
      fetch,
    });

    // Drive the raw openapi-fetch client with a per-call header that has the
    // SAME key.  openapi-fetch places per-call headers on the Request object
    // before running middleware, so authMiddleware sees the header as already
    // present and must NOT overwrite it.
    await sdk.raw.GET('/api/health', {
      headers: { 'x-tenant-id': 'per-call-tenant' },
    });

    const headers = callHeaders(calls[0]!);
    // The per-call value must win; the SDK-level static value must NOT replace it.
    expect(headers['x-tenant-id']).toBe('per-call-tenant');
  });

  it('injects a static header when it is absent from the Request (skip-if-present, false branch)', async () => {
    // Mirrors the previous test for the other arm of the branch: when no
    // per-call header is provided the middleware MUST inject the SDK-level value.
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'x-tenant-id': 'sdk-level-tenant' },
      fetch,
    });

    // No per-call override — header is absent from the Request when middleware runs.
    await sdk.raw.GET('/api/health', {});

    const headers = callHeaders(calls[0]!);
    expect(headers['x-tenant-id']).toBe('sdk-level-tenant');
  });

  it('does not overwrite user-agent when it is already set on the Request', async () => {
    // The user-agent guard uses the same !request.headers.has pattern at the
    // top of onRequest.  A per-call user-agent header triggers that branch.
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch,
    });

    await sdk.raw.GET('/api/health', {
      headers: { 'user-agent': 'custom-agent/1.0' },
    });

    const headers = callHeaders(calls[0]!);
    expect(headers['user-agent']).toBe('custom-agent/1.0');
  });

  it('multiple static headers — only absent ones are injected, present ones are preserved', async () => {
    // Tests that the skip-if-present guard applies independently per header key:
    // one header is pre-set (must be preserved), another is absent (must be injected).
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'x-correlation-id': 'sdk-correlation',
        'x-tenant-id': 'sdk-tenant',
      },
      fetch,
    });

    // Only x-correlation-id is pre-set via per-call headers.
    await sdk.raw.GET('/api/health', {
      headers: { 'x-correlation-id': 'per-call-correlation' },
    });

    const headers = callHeaders(calls[0]!);
    // Pre-set header must be preserved.
    expect(headers['x-correlation-id']).toBe('per-call-correlation');
    // Absent header must be injected from extraHeaders.
    expect(headers['x-tenant-id']).toBe('sdk-tenant');
  });

  // Condition 3 — non-empty value guard (v.length > 0) at the middleware level:
  //   An empty-valued entry cannot reach extraHeaders (it is filtered at the
  //   options.headers normalization step), so the only way to exercise the
  //   middleware-level v.length guard would require bypassing normalization.
  //   The guard is therefore covered implicitly: the existing 'skips
  //   empty-valued static headers' test confirms empty values never enter
  //   extraHeaders, making the in-loop check a defence-in-depth that cannot
  //   be reached via the public API.  These comments document the boundary
  //   so future refactors don't accidentally remove the redundant guard.

  it('runs user-supplied middleware around the auth middleware', async () => {
    const order: string[] = [];
    let bearerSeen = false;
    const res = new Response(JSON.stringify({ status: 'ok' }), { status: 200 });
    const inner = makeFetchForResponse(res);
    const trackingFetch: typeof globalThis.fetch = vi.fn(async (input, init) => {
      order.push('fetch');
      return inner.fetch(input, init as RequestInit | undefined);
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'tok',
      fetch: trackingFetch,
      middleware: [
        {
          async onRequest({ request }) {
            order.push('user-mw-before');
            // Request must already carry Bearer by the time user middleware runs.
            bearerSeen = request.headers.get('authorization') === 'Bearer tok';
            return request;
          },
          async onResponse({ response }) {
            order.push('user-mw-after');
            return response;
          },
        },
      ],
    });
    await sdk.getHealth();
    expect(order).toEqual(['user-mw-before', 'fetch', 'user-mw-after']);
    expect(bearerSeen).toBe(true);
  });
});

describe('createStellarBillClient - authMiddleware accepted input (issue #941)', () => {
  it('success path: accepts a valid token, injects Bearer header, and preserves the documented result', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'valid-token-123',
      fetch,
    });
    const r = await sdk.getHealth();
    // Branch taken: Authorization injected alongside the other middleware headers.
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBe('Bearer valid-token-123');
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    // Documented result behavior is preserved end-to-end.
    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data?.status).toBe('ok');
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toContain('/api/health');
  });

  it('success path: accepts a token rotated via setToken on the next request', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'initial-token',
      fetch,
    });
    sdk.setToken('rotated-token');
    await sdk.getHealth();
    expect(callHeaders(calls[0]!)['authorization']).toBe('Bearer rotated-token');
  });

  it('failure path: absent token leaves Authorization unset and the request still completes', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(r.status).toBe(200);
    expect(r.data?.status).toBe('ok');
  });

  it('failure path: malformed token (internal whitespace) never reaches the Authorization branch', async () => {
    const { fetch, calls } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });
    const r = await sdk.getHealth();
    expect(callHeaders(calls[0]!)['authorization']).toBeUndefined();
    expect(r.status).toBe(200);
  });
});

describe('createStellarBillClient - typed wrappers (success paths)', () => {
  it('getHealth returns parsed data', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data?.status).toBe('ok');
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toContain('/api/health');
  });

  it('does not throw when throwOnError: true and status is 2xx', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(r.error).toBeUndefined();
    expect(r.data?.status).toBe('ok');
  });

  it('listPlans forwards cursor and limit', async () => {
    const { fetch, calls } = mockFetchOnce({ plans: [{ id: 'p1' }], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listPlans({ cursor: 'c', limit: 25 });
    expect(calls[0]!.url).toContain('/api/v1/plans');
    expect(calls[0]!.url).toContain('cursor=c');
    expect(calls[0]!.url).toContain('limit=25');
  });

  it('listSubscriptions forwards cursor and limit', async () => {
    const { fetch, calls } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listSubscriptions({ cursor: 'c', limit: 25 });
    expect(calls[0]!.url).toContain('/api/subscriptions');
    expect(calls[0]!.url).toContain('cursor=c');
    expect(calls[0]!.url).toContain('limit=25');
  });

  it('listSubscriptions omits undefined query params', async () => {
    const { fetch, calls } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.listSubscriptions();
    expect(calls[0]!.url).toContain('/api/subscriptions');
    expect(calls[0]!.url).not.toContain('cursor=');
    expect(calls[0]!.url).not.toContain('limit=');
  });

  it('getSubscription throws on empty id', async () => {
    const { fetch } = mockFetchOnce({ id: 'x', plan_id: 'p', customer: 'c', status: 'a', amount: '1', interval: 'm' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.getSubscription('')).rejects.toBeInstanceOf(StellarBillConfigError);
  });

  it('getSubscription preserves special characters in the path', async () => {
    const { fetch, calls } = mockFetchOnce({ id: 'sub/with spaces', plan_id: 'p' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await sdk.getSubscription('sub/with spaces');
    expect(calls[0]!.url).toContain('/api/subscriptions/');
    expect(decodeURIComponent(calls[0]!.url)).toContain('/api/subscriptions/sub/with spaces');
  });

  it('inspectIdempotencyKey rejects empty key', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.inspectIdempotencyKey('')).rejects.toBeInstanceOf(StellarBillConfigError);
  });

  it('inspectIdempotencyKey rejects key longer than 255 chars', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    await expect(sdk.inspectIdempotencyKey('x'.repeat(256))).rejects.toBeInstanceOf(
      StellarBillConfigError,
    );
  });

  it('inspectIdempotencyKey returns parsed record on success', async () => {
    const { fetch } = mockFetchOnce({
      key: 'abc',
      used_at: '2026-01-01T00:00:00Z',
      expires_at: '2026-01-02T00:00:00Z',
      status_code: 201,
      request_fingerprint: 'fp',
    });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.inspectIdempotencyKey('abc');
    expect(r.data?.status_code).toBe(201);
  });

  it('exposes version, raw client, and token accessors', () => {
    const { fetch } = mockFetchOnce({});
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 't', fetch });
    expect(sdk.version).toMatch(/^\d+\.\d+\.\d+$/);
    expect(typeof sdk.raw).toBe('object');
    expect(sdk.getToken()).toBe('t');
  });

  it('listPlans throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'Bad Request', message: 'bad', code: 'x' }, { status: 400 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.listPlans()).rejects.toMatchObject({ status: 400 });
  });

  it('listSubscriptions returns parsed data on success', async () => {
    const { fetch } = mockFetchOnce({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.listSubscriptions();
    expect(r.data?.subscriptions.length).toBe(0);
  });

  it('getSubscription throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'Not Found', message: 'gone', code: 'missing' }, { status: 404 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.getSubscription('missing')).rejects.toMatchObject({ status: 404 });
  });

  it('inspectIdempotencyKey throws when throwOnError + non-2xx', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Unauthorized', message: 'auth', code: 'auth_unauthorized' },
      { status: 401 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.inspectIdempotencyKey('abc')).rejects.toMatchObject({ status: 401 });
  });
});

describe('createStellarBillClient - error paths (non-2xx)', () => {
  it.each([200, 299])('returns the SDK result envelope at the upper 2xx boundary (%i)', async (status) => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' }, { status });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(status);
    expect(result.data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
    expect(result.error).toBeUndefined();
    expect(result.response.status).toBe(status);
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
  });

  it('returns the parsed error at the first non-2xx boundary when throwing is disabled', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Multiple Choices', message: 'redirect required', code: 'redirect_required' },
      { status: 300 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(300);
    expect(result.data).toBeUndefined();
    expect(result.error).toEqual({
      error: 'Multiple Choices',
      message: 'redirect required',
      code: 'redirect_required',
    });
    expect(result.response.status).toBe(300);
  });

  it('throws at the first non-2xx boundary when throwOnError is enabled', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Multiple Choices', message: 'redirect required', code: 'redirect_required' },
      { status: 300 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    await expect(sdk.getHealth()).rejects.toMatchObject({
      name: 'StellarBillError',
      status: 300,
      body: {
        error: 'Multiple Choices',
        message: 'redirect required',
        code: 'redirect_required',
      },
      requestMethod: 'GET',
      requestUrl: '/api/health',
      message: 'GET /api/health failed (300): redirect required',
    });
  });

  it('returns parsed error in result when not throwOnError', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.listPlans({ limit: 999 });
    expect(r.status).toBe(400);
    expect(r.error?.code).toBe('invalid_cursor');
    expect(r.data).toBeUndefined();
  });

  it('throws StellarBillError when throwOnError: true and status is non-2xx', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Not Found', message: 'gone', code: 'missing' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    let caught: unknown;
    try {
      await sdk.getSubscription('missing-id');
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillError);
    const e = caught as StellarBillError;
    expect(e.status).toBe(404);
    expect(e.requestMethod).toBe('GET');
    expect(e.requestUrl).toContain('/api/subscriptions/');
    expect(e.body?.code).toBe('missing');
  });

  it('rejects an invalid cursor with the request and actionable API error details', async () => {
    const body = { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' };
    const { fetch, calls } = mockFetchOnce(body, { status: 400 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'sdk-token',
      throwOnError: true,
      fetch,
    });

    let caught: unknown;
    try {
      await sdk.listPlans({ cursor: 'bad cursor' });
    } catch (error) {
      caught = error;
    }

    expect(calls).toHaveLength(1);
    expect(new URL(calls[0]!.url).searchParams.get('cursor')).toBe('bad cursor');
    expect(callHeaders(calls[0]!)['authorization']).toBe('Bearer sdk-token');
    expect(caught).toBeInstanceOf(StellarBillError);
    const rejected = caught as StellarBillError;
    expect(rejected).toMatchObject({
      status: 400,
      body,
      requestMethod: 'GET',
      requestUrl: '/api/v1/plans',
      message: 'GET /api/v1/plans failed (400): Invalid cursor',
    });
    expect(rejected.message).not.toContain('sdk-token');
  });

  it('returns a successful plans response when throwOnError is enabled', async () => {
    const { fetch } = mockFetchOnce({ plans: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const result = await sdk.listPlans({ cursor: 'valid' });
    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data?.plans).toEqual([]);
  });

  it('non-2xx with non-JSON content returns undefined error body', async () => {
    const { fetch } = mockFetchOnce('<html>nope</html>', { status: 500, contentType: 'text/html' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 500,
      body: undefined,
    });
  });

  it('non-2xx with unparseable JSON content returns undefined error body', async () => {
    const { fetch } = mockFetchOnce('not-valid-json', { status: 502, contentType: 'application/json' });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });
    await expect(sdk.getHealth()).rejects.toMatchObject({
      status: 502,
      body: undefined,
    });
  });
});

describe('isLocalhost accepted input (issue #881)', () => {
  // --- Success paths: representative valid inputs accepted by the branch ---
  it('returns true for the localhost hostname', () => {
    expect(isLocalhost('http://localhost:8080/api')).toBe(true);
  });

  it('returns true for the 127.0.0.1 loopback address', () => {
    expect(isLocalhost('http://127.0.0.1:8080')).toBe(true);
  });

  it('returns false for a remote hostname', () => {
    expect(isLocalhost('https://api.stellabill.com')).toBe(false);
  });

  it('returns false for an IP that merely contains the loopback octets', () => {
    // Boundary: "127.0.0.1" must match exactly, not as a substring.
    expect(isLocalhost('http://127.0.0.10:8080')).toBe(false);
    expect(isLocalhost('http://10.0.0.127:8080')).toBe(false);
  });

  it('returns false for a hostname that merely contains the word localhost', () => {
    expect(isLocalhost('http://localhost.example.com:8080')).toBe(false);
    expect(isLocalhost('http://notlocalhost:8080')).toBe(false);
  });

  it('is case-insensitive on hostnames (URL parsing normalizes the host)', () => {
    expect(isLocalhost('http://LOCALHOST:8080')).toBe(true);
    expect(isLocalhost('http://LocalHost')).toBe(true);
  });

  it('ignores port, path, query, and credentials', () => {
    expect(isLocalhost('http://user:pass@localhost:9999/x?y=1')).toBe(true);
    expect(isLocalhost('http://127.0.0.1/deep/path?x=1')).toBe(true);
  });

  it('checks the hostname regardless of scheme (WHATWG special schemes parse the same)', () => {
    expect(isLocalhost('ftp://127.0.0.1')).toBe(true);
    expect(isLocalhost('ws://localhost')).toBe(true);
  });

  // --- Failure path: the `return false` catch branch at client.ts:104 ---
  it('returns false for a malformed URL instead of throwing', () => {
    expect(isLocalhost('not-a-url')).toBe(false);
  });

  it('returns false for empty and whitespace-only inputs', () => {
    expect(isLocalhost('')).toBe(false);
    expect(isLocalhost('   ')).toBe(false);
  });
});

describe('createStellarBillClient - documented error-handling pattern (client.ts:142)', () => {
  // Line 142 of src/client.ts is the `if (error) throw new Error(error.message);`
  // guard inside the `@example` JSDoc on `createStellarBillClient`, which
  // documents how consumers handle the `SdkResult` returned by every public
  // wrapper (the example itself uses `sdk.getHealth()`). These tests execute
  // that exact pattern against the public API surface so both branches of the
  // documented example stay observable and stable. Mocked fetch only — no
  // network, fully deterministic.

  /** HealthResponse example values from openapi/openapi.yaml. */
  const VALID_HEALTH_BODY = { status: 'ok', service: 'stellarbill-backend' } as const;

  it('accepted input: no throw and documented result when error is absent', async () => {
    const { fetch } = mockFetchOnce(VALID_HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const { data, error, status, requestMethod, requestUrl } = await sdk.getHealth();

    // Accepted-input branch of the documented pattern: the guard is a no-op
    // when `error` is absent.
    expect(error).toBeUndefined();
    const documentedPattern = () => {
      if (error) throw new Error(error.message);
      return data;
    };
    expect(documentedPattern).not.toThrow();

    // The result is the documented shape/value, not merely "didn't throw":
    // full HealthResponse body plus the SdkResult envelope fields.
    expect(data).toEqual({ status: 'ok', service: 'stellarbill-backend' });
    expect(status).toBe(200);
    expect(requestMethod).toBe('GET');
    expect(requestUrl).toContain('/api/health');
  });

  it('error path: throws an Error whose message is exactly error.message', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const { error } = await sdk.getHealth();
    expect(error).toBeDefined();
    expect(error?.message).toBe('Invalid cursor');

    let caught: unknown;
    try {
      // The documented pattern (client.ts:142), verbatim.
      if (error) throw new Error(error.message);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(Error);
    // The example deliberately throws the platform Error (throw-on-error via
    // `throwOnError`/`assertOk` throws StellarBillError instead) — the message
    // must be propagated verbatim, not swallowed or replaced.
    expect(caught).not.toBeInstanceOf(StellarBillError);
    expect((caught as Error).message).toBe('Invalid cursor');
  });

  it('boundary: error present without message propagates as an empty-string message', async () => {
    // `ApiErrorBody.message` is optional, so an envelope carrying only
    // `error` is type-legal and reaches this branch in practice (e.g. a
    // proxy-generated 5xx body). The falsy-error variants (0/"") are not
    // type-legal here and are deliberately not covered.
    const { fetch } = mockFetchOnce({ error: 'Service Unavailable' }, { status: 503 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const { error } = await sdk.getHealth();
    expect(error).toBeDefined();
    expect(error?.message).toBeUndefined();

    let caught: unknown;
    try {
      if (error) throw new Error(error.message);
    } catch (err) {
      caught = err;
    }
    // `new Error(undefined)` normalizes to an empty message: the documented
    // example never crashes on a message-less envelope.
    expect(caught).toBeInstanceOf(Error);
    expect((caught as Error).message).toBe('');
  });
});

describe('createStellarBillClient - warning path coverage', () => {
  it('does not crash when console is fully unavailable', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const savedConsole = globalThis.console;
    (globalThis as { console?: Console }).console = undefined as unknown as Console;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      const r = await sdk.getHealth();
      expect(r.status).toBe(200);
    } finally {
      (globalThis as { console?: Console }).console = savedConsole;
    }
  });

  it('skips warning when console.warn is missing', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const savedWarn = console.warn;
    (console as unknown as { warn?: () => void }).warn = undefined;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      const r = await sdk.getHealth();
      expect(r.status).toBe(200);
    } finally {
      console.warn = savedWarn;
    }
  });
});

describe('assertOk', () => {
  it('returns data when 2xx and data present', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    const data = await assertOk(r);
    expect(data.status).toBe('ok');
  });

  it('throws when 2xx but body has no parsed data', async () => {
    // openapi-fetch always calls res.json(), so the SDK cannot produce
    // a `data === undefined` with status 200. Construct the SdkResult
    // inline to exercise the assertOk branch directly.
    const r = {
      data: undefined,
      error: undefined,
      status: 200,
      response: new Response('{}', { status: 200 }),
      requestMethod: 'GET',
      requestUrl: '/test',
    };
    await expect(assertOk(r)).rejects.toThrow(/empty body/);
  });

  it('throws StellarBillError on non-2xx', async () => {
    const { fetch } = mockFetchOnce({ error: 'oops' }, { status: 500 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    await expect(assertOk(r)).rejects.toBeInstanceOf(StellarBillError);
    await expect(assertOk(r)).rejects.toMatchObject({ status: 500 });
  });
});

describe('safeParseErrorBody', () => {
  it('returns undefined when content-type is missing', async () => {
    const r = new Response('{}', { status: 400 });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when content-type is not JSON', async () => {
    const r = new Response('oops', { status: 400, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when body is empty', async () => {
    const r = new Response('', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined at text boundary condition when empty', async () => {
    // This test specifically exercises the boundary condition at line 113: if (!text) return undefined;
    // It verifies that empty text (falsy value) is handled correctly and returns undefined
    // without attempting JSON parsing, ensuring the boundary check is observable.
    const r = new Response('', { status: 400, headers: { 'content-type': 'application/json' } });
    // This should hit the !text check at line 113 and return undefined immediately
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('parses a valid error body', async () => {
    const r = new Response(JSON.stringify({ message: 'bad', code: 'x' }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    expect(await safeParseErrorBody(r)).toEqual({ message: 'bad', code: 'x' });
  });

  it('accepts and returns parsed object when valid ApiErrorBody (line 115 branch)', async () => {
    // This test specifically exercises the accepted input branch at line 115:
    // if (parsed && typeof parsed === 'object' && !Array.isArray(parsed))
    // It verifies that valid objects with error fields are accepted and returned.
    const errorBody = {
      error: 'Bad Request',
      message: 'Invalid input provided',
      code: 'validation_error',
    };
    const r = new Response(JSON.stringify(errorBody), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    const result = await safeParseErrorBody(r);
    expect(result).toEqual(errorBody);
    expect(result?.error).toBe('Bad Request');
    expect(result?.message).toBe('Invalid input provided');
    expect(result?.code).toBe('validation_error');
  });

  it('returns undefined on invalid JSON', async () => {
    const r = new Response('not-json', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is a string', async () => {
    const r = new Response('"a string"', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is a number', async () => {
    const r = new Response('42', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is an array', async () => {
    const r = new Response('[1,2,3]', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is null', async () => {
    const r = new Response('null', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when status 200 with empty body and non-json content', async () => {
    const r = new Response('', { status: 200, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when res.text() throws', async () => {
    const r = new Response('ok', { status: 400, headers: { 'content-type': 'application/json' } });
    vi.spyOn(r, 'text').mockRejectedValue(new Error('boom'));
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when JSON.parse() throws on valid text content', async () => {
    // This test ensures the catch block (line 119) handles JSON.parse errors gracefully.
    // We spy on JSON.parse to simulate a parsing error and verify the function
    // returns undefined instead of throwing.
    const r = new Response('{"message":"test"}', {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
    vi.spyOn(JSON, 'parse').mockImplementation(() => {
      throw new SyntaxError('Unexpected token');
    });
    try {
      expect(await safeParseErrorBody(r)).toBeUndefined();
    } finally {
      vi.restoreAllMocks();
    }
  });
});

// Issue #897 — dedicated rejected-input coverage for the branch that ends in
// `return parsed as ApiErrorBody;` (src/client.ts:116).
//
// Line 115's guard (`parsed && typeof parsed === 'object' && !Array.isArray(parsed)`)
// is the only thing between an unchecked JSON.parse() result and the ApiErrorBody
// contract this exported helper advertises. The suites above and in
// client-safe-parse-error-body.test.ts already pin strings, arrays, null, plain
// numbers and malformed JSON, so what remains unpinned is: booleans (truthy but
// non-object), the falsy primitives that never reach the `typeof` clause at all,
// and the fact that the guard is shallow rather than value-aware.
describe('safeParseErrorBody - remaining guard clauses for client.ts:116', () => {
  const json = (text: string): Response =>
    new Response(text, { status: 400, headers: { 'content-type': 'application/json' } });

  it('rejects a JSON boolean: truthy, but never satisfies `typeof parsed === "object"`', async () => {
    expect(await safeParseErrorBody(json('true'))).toBeUndefined();
  });

  it('rejects falsy JSON primitives that short-circuit `parsed &&` before typeof runs', async () => {
    // `false`, `0` and `""` are legal JSON values that the `typeof` and
    // `Array.isArray` clauses would otherwise probe: the guard has to refuse
    // them on truthiness alone, and the result must be indistinguishable from
    // every other rejection (undefined, never a partial body).
    expect(await safeParseErrorBody(json('false'))).toBeUndefined();
    expect(await safeParseErrorBody(json('0'))).toBeUndefined();
    expect(await safeParseErrorBody(json('""'))).toBeUndefined();
  });

  it('accepts an object with null field values — the guard only inspects the top level', async () => {
    // Contrast with the rejected `null` payload above: null *values* inside an
    // object still reach client.ts:116, so callers must be able to read them
    // (and the SDK's message builder falls through them via `??`).
    const parsed = await safeParseErrorBody(json('{"message":null,"code":null}'));
    expect(parsed).toEqual({ message: null, code: null });
    expect(parsed?.message ?? 'HTTP 400').toBe('HTTP 400');
    expect(parsed?.code ?? 'unknown').toBe('unknown');
  });
});

// The other half of the contract: what callers observe when the line 115/116
// guard rejects the body. Rejection must never fabricate an ApiErrorBody —
// `error` stays undefined and the message degrades to a stable `HTTP <status>`
// while still identifying method, URL and status.
describe('error contract for rejected response bodies (client.ts:115/116)', () => {
  it('keeps `error` undefined, still exposes the response, and falls back to `HTTP <status>`', async () => {
    // openapi-fetch JSON-parses every non-2xx body, so a bare JSON number is
    // handed to the SDK as `error: 42`; the guard must refuse it instead of
    // casting it into an ApiErrorBody.
    const { fetch } = mockFetchOnce(42, { status: 400 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();

    expect(r.status).toBe(400);
    expect(r.error).toBeUndefined();
    expect(r.response).toBeInstanceOf(Response);
    expect(r.response.status).toBe(400);

    const err = (await assertOk(r).catch((e: unknown) => e)) as StellarBillError;
    expect(err).toBeInstanceOf(StellarBillError);
    expect(err.message).toBe('GET /api/health failed (400): HTTP 400');
    expect(err.body).toBeUndefined();
    expect(err.toString()).toContain('(unknown)');
  });

  it('throwOnError reports the same deterministic contract for a falsy body', async () => {
    // `0` is refused by the `parsed &&` short-circuit, and openapi-fetch's own
    // object check drops it first — either way no body may be invented.
    const { fetch } = mockFetchOnce(0, { status: 500 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch,
    });

    const err = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;
    expect(err).toBeInstanceOf(StellarBillError);
    expect(err.status).toBe(500);
    expect(err.body).toBeUndefined();
    expect(err.message).toBe('GET /api/health failed (500): HTTP 500');
  });
});

describe('Token integration with createStellarBillClient', () => {
  it('handles basic TokenHolder behavior via the SDK', async () => {
    const { fetch } = mockFetchOnce({});
    const sdk1 = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    expect(sdk1.getToken()).toBeUndefined();
    sdk1.setToken('a');
    expect(sdk1.getToken()).toBe('a');
    sdk1.setToken(undefined);
    expect(sdk1.getToken()).toBeUndefined();
  });
});

/**
 * Boundary conditions for the `throwOnError` option (issue #850,
 * `src/client.ts:21` -> enforced in `wrap()` at `src/client.ts:228`).
 *
 * The option is documented as "throw StellarBillError on any non-2xx
 * response. Default false". The guard is:
 *
 * ```ts
 * if (throwOnError && (status < 200 || status >= 300)) { throw ... }
 * ```
 *
 * Two things are easy to get wrong and are therefore pinned explicitly here:
 *
 * 1. The enabled flag is read with **`=== true`**, not truthiness, so any
 *    non-`true` value (including a truthy one) selects envelope semantics.
 * 2. The exact edges are `status < 200` and `status >= 300` — 200 and 299 are
 *    successes and must NOT throw; 300 is the first throwing status.
 */
describe('createStellarBillClient - throwOnError boundary conditions (#850)', () => {
  /** Build a client whose response has a synthetic absolute URL + status. */
  function respondWith(
    body: unknown,
    opts: { status: number; url?: string; contentType?: string },
  ): { fetch: typeof globalThis.fetch } {
    const text = typeof body === 'string' ? body : JSON.stringify(body);
    const res = new Response(text, {
      status: Math.min(Math.max(opts.status, 200), 599),
      headers: { 'content-type': opts.contentType ?? 'application/json' },
    });
    // `Response.status`/`.url` are prototype getters; a real fetch() response
    // would carry the final absolute URL and (for opaque responses) status 0,
    // neither of which the Response constructor can produce.
    Object.defineProperty(res, 'status', { value: opts.status, configurable: true });
    if (opts.url !== undefined) {
      Object.defineProperty(res, 'url', { value: opts.url, configurable: true });
    }
    return makeFetchForResponse(res);
  }

  // ── The `status >= 300` edge ──────────────────────────────────────────────

  it('does not throw at status 200 when throwOnError is enabled', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' }, { status: 200 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(r.data?.status).toBe('ok');
    expect(r.error).toBeUndefined();
  });

  it('does not throw at status 299 (last 2xx) when throwOnError is enabled', async () => {
    const { fetch } = mockFetchOnce({ status: 'ok', service: 'stellarbill-backend' }, { status: 299 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(299);
    expect(r.data?.status).toBe('ok');
  });

  it('throws at status 300 (first non-2xx) when throwOnError is enabled', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Multiple Choices', message: 'redirect-ish', code: 'ambiguous' },
      { status: 300 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = await sdk.listPlans().catch((e: unknown) => e);
    expect(caught).toBeInstanceOf(StellarBillError);
    expect((caught as StellarBillError).status).toBe(300);
  });

  it('returns an envelope at status 300 when throwOnError is disabled', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Multiple Choices', message: 'redirect-ish', code: 'ambiguous' },
      { status: 300 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: false, fetch });
    const r = await sdk.listPlans();
    expect(r.status).toBe(300);
    expect(r.error?.code).toBe('ambiguous');
    expect(r.data).toBeUndefined();
  });

  // ── The `status < 200` edge ───────────────────────────────────────────────

  it('throws at status 0 (opaque response) when throwOnError is enabled', async () => {
    const { fetch } = respondWith({ code: 'opaque' }, { status: 0 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = await sdk.getHealth().catch((e: unknown) => e);
    expect(caught).toBeInstanceOf(StellarBillError);
    expect((caught as StellarBillError).status).toBe(0);
  });

  it('throws at status 199 (last value below 200) when throwOnError is enabled', async () => {
    const { fetch } = respondWith({ status: 'informational' }, { status: 199 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = await sdk.getHealth().catch((e: unknown) => e);
    expect(caught).toBeInstanceOf(StellarBillError);
    expect((caught as StellarBillError).status).toBe(199);
  });

  // ── The `=== true` strict-equality boundary ───────────────────────────────

  it('treats an omitted throwOnError as false (default)', async () => {
    const { fetch } = mockFetchOnce({ error: 'nope' }, { status: 500 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(500);
    expect(r.error?.error).toBe('nope');
  });

  it('treats an explicit throwOnError: false as false', async () => {
    const { fetch } = mockFetchOnce({ error: 'nope' }, { status: 500 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: false, fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(500);
  });

  it('treats a truthy non-boolean throwOnError as false (strict === true comparison)', async () => {
    // `options.throwOnError === true` means JS truthiness is deliberately NOT
    // used: 1, 'true', {} etc. all select envelope semantics. Pinned so the
    // comparison operator cannot be relaxed without this test failing.
    const { fetch } = mockFetchOnce({ error: 'nope' }, { status: 500 });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: 1 as unknown as boolean,
      fetch,
    });
    const r = await sdk.getHealth();
    expect(r.status).toBe(500);
    expect(r.error?.error).toBe('nope');
  });

  // ── Thrown error shape + message fallback chain ───────────────────────────

  it('populates every StellarBillError field and the message from body.message', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Not Found', message: 'gone', code: 'missing' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = (await sdk.getSubscription('abc').catch((e: unknown) => e)) as StellarBillError;

    expect(caught).toBeInstanceOf(StellarBillError);
    expect(caught.name).toBe('StellarBillError');
    expect(caught.status).toBe(404);
    expect(caught.requestMethod).toBe('GET');
    expect(caught.requestUrl).toBe('/api/subscriptions/abc');
    expect(caught.body).toEqual({ error: 'Not Found', message: 'gone', code: 'missing' });
    expect(caught.message).toBe('GET /api/subscriptions/abc failed (404): gone');
    expect(caught.toString()).toContain('404 (missing)');
  });

  it('falls back to body.error when body.message is absent', async () => {
    const { fetch } = mockFetchOnce({ error: 'Bad Request' }, { status: 400 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = (await sdk.listPlans().catch((e: unknown) => e)) as StellarBillError;
    expect(caught.message).toBe('GET /api/v1/plans failed (400): Bad Request');
  });

  it('falls back to `HTTP <status>` when the body is unusable', async () => {
    const { fetch } = mockFetchOnce('<html>nope</html>', { status: 500, contentType: 'text/html' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;
    expect(caught.status).toBe(500);
    expect(caught.body).toBeUndefined();
    expect(caught.message).toBe('GET /api/health failed (500): HTTP 500');
  });

  it('drops a non-object error payload from the envelope but still throws', async () => {
    // openapi-fetch surfaces a string body verbatim as `error`; the SDK only
    // accepts objects as ApiErrorBody, so `error`/`body` become undefined while
    // the non-2xx status still throws.
    const { fetch } = mockFetchOnce('"boom"', { status: 502 });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', throwOnError: true, fetch });
    const caught = (await sdk.getHealth().catch((e: unknown) => e)) as StellarBillError;
    expect(caught).toBeInstanceOf(StellarBillError);
    expect(caught.status).toBe(502);
    expect(caught.body).toBeUndefined();

    const { fetch: fetch2 } = mockFetchOnce('"boom"', { status: 502 });
    const sdk2 = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: fetch2 });
    const r = await sdk2.getHealth();
    expect(r.status).toBe(502);
    expect(r.error).toBeUndefined();
  });

  // ── requestUrl asymmetry between the two modes ────────────────────────────

  it('exposes the absolute URL in the envelope but the relative path in the thrown error', async () => {
    // Characterization test for a known inconsistency (follow-up in the PR):
    // `wrap()` returns `response.url || urlPath` but throws with `urlPath`.
    // With a real fetch the envelope therefore carries the fully-resolved URL
    // (query string included) while the error carries only the path template.
    const absoluteUrl = 'https://api.example.com/api/v1/plans?cursor=c&limit=25';

    const { fetch: envelopeFetch } = respondWith(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400, url: absoluteUrl },
    );
    const envelopeSdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: envelopeFetch });
    const envelope = await envelopeSdk.listPlans({ cursor: 'c', limit: 25 });
    expect(envelope.requestUrl).toBe(absoluteUrl);

    const { fetch: throwFetch } = respondWith(
      { error: 'Bad Request', message: 'Invalid cursor', code: 'invalid_cursor' },
      { status: 400, url: absoluteUrl },
    );
    const throwSdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      fetch: throwFetch,
    });
    const caught = (await throwSdk.listPlans({ cursor: 'c', limit: 25 }).catch((e: unknown) => e)) as StellarBillError;

    // Documents today's behaviour: the error loses the absolute URL and the
    // query string that the envelope preserves.
    expect(caught.requestUrl).toBe('/api/v1/plans');
    expect(caught.requestUrl).not.toContain('limit=');
  });

  // ── Determinism ───────────────────────────────────────────────────────────

  it('produces byte-identical error fields for identical inputs', async () => {
    async function capture(): Promise<Record<string, unknown>> {
      const { fetch } = mockFetchOnce({ error: 'E', message: 'M', code: 'C' }, { status: 409 });
      const sdk = createStellarBillClient({
        baseUrl: 'https://api.example.com',
        throwOnError: true,
        fetch,
      });
      const err = (await sdk.listSubscriptions().catch((e: unknown) => e)) as StellarBillError;
      return {
        status: err.status,
        body: err.body,
        requestMethod: err.requestMethod,
        requestUrl: err.requestUrl,
        message: err.message,
        stringified: err.toString(),
      };
    }

    const first = await capture();
    const second = await capture();
    expect(second).toEqual(first);
  });

  it('never throws for non-2xx on every wrapped operation when throwOnError is off', async () => {
    // A fresh mock per operation: each mocked Response body can only be read once.
    const cases: Array<() => Promise<{ status: number; error: unknown; data: unknown }>> = [
      () =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: mockFetchOnce({ error: 'E' }, { status: 500 }).fetch,
        }).getHealth(),
      () =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: mockFetchOnce({ error: 'E' }, { status: 500 }).fetch,
        }).listPlans(),
      () =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: mockFetchOnce({ error: 'E' }, { status: 500 }).fetch,
        }).listSubscriptions(),
      () =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: mockFetchOnce({ error: 'E' }, { status: 500 }).fetch,
        }).getSubscription('id'),
      () =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: mockFetchOnce({ error: 'E' }, { status: 500 }).fetch,
        }).inspectIdempotencyKey('key'),
    ];

    for (const run of cases) {
      const r = await run();
      expect(r.status).toBe(500);
      expect(r.data).toBeUndefined();
      expect(r.error).toEqual({ error: 'E' });
    }
  });
});
