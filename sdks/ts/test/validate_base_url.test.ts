import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

/**
 * Focused coverage for `validateBaseUrl` (sdks/ts/src/client.ts).
 *
 * Named evidence from the issue:
 *   - `sdks/ts/src/client.ts:93` → `throw ... \`baseUrl "${raw}" is not a valid URL\`;`
 *
 * That throw is only reachable for inputs the WHATWG `URL` constructor rejects.
 * The accepted branch is the one clients depend on, so these tests pin both the
 * representative accepted inputs (and the normalization they produce at request
 * time) and the exact error contract of the rejection branch.
 */

type FetchCall = { url: string };

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

function mockFetchOnce(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const response = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });
  const fetch: typeof globalThis.fetch = vi.fn(async (input) => {
    calls.push({ url: toUrl(input) });
    return response;
  });
  return { fetch, calls };
}

async function resolveBaseUrl(baseUrl: string): Promise<string> {
  const { fetch, calls } = mockFetchOnce();
  const sdk = createStellarBillClient({ baseUrl, fetch });
  await sdk.getHealth();
  expect(calls).toHaveLength(1);
  return calls[0]!.url;
}

describe('validateBaseUrl - accepted input reaches the request layer', () => {
  it('accepts a canonical https origin', async () => {
    await expect(resolveBaseUrl('https://api.example.com')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });

  it('accepts and strips a single trailing slash', async () => {
    await expect(resolveBaseUrl('https://api.example.com/')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });

  it('accepts and strips multiple trailing slashes', async () => {
    await expect(resolveBaseUrl('https://api.example.com///')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });

  it('normalizes an uppercase scheme and host', async () => {
    await expect(resolveBaseUrl('HTTPS://API.EXAMPLE.COM')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });

  it('drops the explicit default https port', async () => {
    await expect(resolveBaseUrl('https://api.example.com:443/')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });

  it('preserves a non-default port', async () => {
    await expect(resolveBaseUrl('https://api.example.com:8443/')).resolves.toBe(
      'https://api.example.com:8443/api/health',
    );
  });

  it('accepts a local http origin on a non-default port', async () => {
    await expect(resolveBaseUrl('http://127.0.0.1:8080/')).resolves.toBe(
      'http://127.0.0.1:8080/api/health',
    );
  });

  it('accepts input wrapped in surrounding whitespace', async () => {
    await expect(resolveBaseUrl('  https://api.example.com  ')).resolves.toBe(
      'https://api.example.com/api/health',
    );
  });
});

describe('validateBaseUrl - rejection branch (client.ts:93)', () => {
  const rejected = ['not-a-url', 'http://', 'https://', '//missing-scheme', 'ht tp://example.com'];

  it.each(rejected)('rejects %j with a StellarBillConfigError', (baseUrl) => {
    expect(() => createStellarBillClient({ baseUrl })).toThrow(StellarBillConfigError);
  });

  it('includes the raw input in the error message', () => {
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url' })).toThrow(
      'baseUrl "not-a-url" is not a valid URL',
    );
  });

  it('does not issue any request when construction fails', () => {
    const { fetch, calls } = mockFetchOnce();
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url', fetch })).toThrow(
      StellarBillConfigError,
    );
    expect(calls).toHaveLength(0);
  });

  it('is an instanceof Error so callers can catch it generically', () => {
    try {
      createStellarBillClient({ baseUrl: 'not-a-url' });
      throw new Error('expected createStellarBillClient to throw');
    } catch (error) {
      expect(error).toBeInstanceOf(StellarBillConfigError);
      expect(error).toBeInstanceOf(Error);
    }
  });
});

describe('validateBaseUrl - non-string and empty boundaries', () => {
  it.each([undefined, null])('requires baseUrl when it is %s', (baseUrl) => {
    expect(() =>
      createStellarBillClient({ baseUrl: baseUrl as unknown as string }),
    ).toThrow(/baseUrl is required/);
  });

  it.each([42, {}, [], true])('rejects the non-string baseUrl %j', (baseUrl) => {
    expect(() =>
      createStellarBillClient({ baseUrl: baseUrl as unknown as string }),
    ).toThrow(/non-empty string/);
  });

  it.each(['', '   ', '\t\n'])('rejects the blank baseUrl %j', (baseUrl) => {
    expect(() => createStellarBillClient({ baseUrl })).toThrow(/non-empty string/);
  });
});
