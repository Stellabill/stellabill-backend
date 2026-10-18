import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

/**
 * Dedicated coverage for the `baseUrl` validation / rejection branches in
 * `src/client.ts:149` (the compound "insecure baseUrl" condition) plus the
 * `validateBaseUrl` guard clauses that feed it.
 */

function stubFetch() {
  const calls: string[] = [];
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    void init;
    calls.push(input instanceof Request ? input.url : String(input));
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, calls };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - baseUrl rejected input', () => {
  it.each<[string, unknown, string]>([
    ['undefined', undefined, 'baseUrl is required'],
    ['null', null, 'baseUrl is required'],
    ['an empty string', '', 'baseUrl must be a non-empty string'],
    ['a single space', ' ', 'baseUrl must be a non-empty string'],
    ['a tab + newline', '\t\n', 'baseUrl must be a non-empty string'],
    ['a number', 42, 'baseUrl must be a non-empty string'],
    ['a boolean', true, 'baseUrl must be a non-empty string'],
    ['a symbol', Symbol('baseUrl'), 'baseUrl must be a non-empty string'],
    ['an array', [], 'baseUrl must be a non-empty string'],
    ['a plain object', {}, 'baseUrl must be a non-empty string'],
    ['a function', () => 'https://api.example.com', 'baseUrl must be a non-empty string'],
    ['a URL instance', new URL('https://api.example.com'), 'baseUrl must be a non-empty string'],
  ])('rejects %s with an actionable config error', (_label, value, message) => {
    const { fetch } = stubFetch();

    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: value as string, fetch });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe(message);
  });

  it.each([
    ['a bare hostname without a scheme', 'api.example.com'],
    ['a scheme-only string', 'https://'],
    ['an http scheme with no host', 'http://'],
    ['a leading colon', '://example.com'],
    ['an embedded space', 'https://exa mple.com'],
    ['free-form text', 'not-a-url'],
    ['a relative path', '/api/v1'],
  ])('rejects %s as an invalid URL', (_label, value) => {
    const { fetch } = stubFetch();

    expect(() => createStellarBillClient({ baseUrl: value, fetch })).toThrow(
      new StellarBillConfigError(`baseUrl "${value}" is not a valid URL`),
    );
  });

  it('does not invoke fetch or warn when baseUrl is rejected', () => {
    const { fetch, calls } = stubFetch();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    expect(() => createStellarBillClient({ baseUrl: 'not-a-url', fetch })).toThrow(
      StellarBillConfigError,
    );

    expect(calls).toHaveLength(0);
    expect(warn).not.toHaveBeenCalled();
  });
});

describe('createStellarBillClient - baseUrl accepted boundaries', () => {
  it('normalizes a whitespace-padded URL instead of rejecting it', async () => {
    const { fetch, calls } = stubFetch();

    const sdk = createStellarBillClient({ baseUrl: '  https://api.example.com  ', fetch });
    await sdk.getHealth();

    expect(calls[0]).toBe('https://api.example.com/api/health');
  });

  it('lowercases the scheme/host and keeps the path', async () => {
    const { fetch, calls } = stubFetch();

    const sdk = createStellarBillClient({ baseUrl: 'HTTPS://API.Example.COM/v1', fetch });
    await sdk.getHealth();

    expect(calls[0]).toBe('https://api.example.com/v1/api/health');
  });

  it('strips every trailing slash but preserves an explicit path', async () => {
    const { fetch, calls } = stubFetch();

    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/v1///', fetch });
    await sdk.getHealth();

    expect(calls[0]).toBe('https://api.example.com/v1/api/health');
  });

  it('accepts a non-http(s) scheme without emitting an insecure warning', () => {
    const { fetch } = stubFetch();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const sdk = createStellarBillClient({ baseUrl: 'ftp://files.example.com', fetch });

    expect(sdk.version).toMatch(/^\d+\.\d+\.\d+$/);
    expect(warn).not.toHaveBeenCalled();
  });

  it('accepts credentials in the URL and does not warn over https', () => {
    const { fetch } = stubFetch();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    createStellarBillClient({ baseUrl: 'https://user:pass@api.example.com', fetch });

    expect(warn).not.toHaveBeenCalled();
  });
});

describe('createStellarBillClient - insecure baseUrl warning branch (client.ts:149)', () => {
  it.each([
    ['a plain remote http host', 'http://example.com', 'http://example.com'],
    ['an http host with a path', 'http://example.com/v1', 'http://example.com/v1'],
    ['an uppercased http scheme', 'HTTP://EXAMPLE.COM', 'http://example.com'],
    ['an http host with a non-default port', 'http://example.com:8080/', 'http://example.com:8080'],
    ['an IPv6 loopback (only localhost/127.0.0.1 are exempt)', 'http://[::1]:8080', 'http://[::1]:8080'],
    ['an adjacent loopback address', 'http://127.0.0.2', 'http://127.0.0.2'],
  ])('warns for %s', (_label, baseUrl, normalized) => {
    const { fetch } = stubFetch();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    createStellarBillClient({ baseUrl, fetch });

    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(
      `[stellabill-sdk] Insecure baseUrl "${normalized}" - use https:// in production`,
    );
  });

  it.each([
    ['an https remote host', 'https://example.com'],
    ['localhost over http', 'http://localhost'],
    ['localhost with a port over http', 'http://localhost:8080'],
    ['the 127.0.0.1 loopback over http', 'http://127.0.0.1'],
    ['the 127.0.0.1 loopback with a port over http', 'http://127.0.0.1:4321'],
  ])('does not warn for %s', (_label, baseUrl) => {
    const { fetch } = stubFetch();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    createStellarBillClient({ baseUrl, fetch });

    expect(warn).not.toHaveBeenCalled();
  });

  it('skips the warning entirely when console is unavailable', () => {
    const { fetch } = stubFetch();
    const saved = globalThis.console;
    (globalThis as { console?: Console }).console = undefined as unknown as Console;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      expect(sdk.version).toMatch(/^\d+\.\d+\.\d+$/);
    } finally {
      (globalThis as { console?: Console }).console = saved;
    }
  });

  it('skips the warning when console.warn is missing', () => {
    const { fetch } = stubFetch();
    const saved = console.warn;
    (console as unknown as { warn?: () => void }).warn = undefined;
    try {
      const sdk = createStellarBillClient({ baseUrl: 'http://example.com', fetch });
      expect(sdk.version).toMatch(/^\d+\.\d+\.\d+$/);
    } finally {
      console.warn = saved;
    }
  });
});
