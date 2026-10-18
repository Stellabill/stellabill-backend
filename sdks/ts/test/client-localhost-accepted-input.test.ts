import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

/**
 * Accepted-input coverage for the loopback guard reached from the insecure
 * `http://` baseUrl warning (`sdks/ts/src/client.ts:102`):
 *
 *   const u = new URL(baseUrl);
 *   return u.hostname === 'localhost' || u.hostname === '127.0.0.1';
 *
 * This is the "*accepts a representative valid input*" half of the branch:
 * every spelling the guard is documented to accept must suppress the
 * `Insecure baseUrl` warning, while the client keeps working normally. The
 * rejection / look-alike boundary cases are covered separately; these tests
 * deliberately pin the positive domain only.
 */

type FetchCall = {
  url: string;
  headers: Record<string, string>;
};

function mockFetch(
  body: unknown,
  init: { status?: number } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const text = body === undefined ? '' : JSON.stringify(body);
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: input instanceof Request ? input.url : String(input), headers });
    // A fresh Response per call: `Body has already been read` otherwise.
    return new Response(text, { status, headers: { 'content-type': 'application/json' } });
  });
  return { fetch, calls };
}

const HEALTH = { status: 'ok', service: 'stellarbill-backend' };

afterEach(() => {
  vi.restoreAllMocks();
});

describe('isLocalhost() - accepted loopback inputs do not warn', () => {
  const accepted = [
    'http://localhost',
    'http://localhost/',
    'http://localhost:3000',
    'http://localhost:8080/api/v1/health',
    'http://localhost?probe=1',
    'http://127.0.0.1',
    'http://127.0.0.1:65535',
    'http://127.0.0.1/',
  ];

  for (const baseUrl of accepted) {
    it(`does not warn for ${baseUrl}`, () => {
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
      const { fetch } = mockFetch(HEALTH);
      createStellarBillClient({ baseUrl, fetch });
      expect(warn).not.toHaveBeenCalled();
    });
  }

  it('accepts a loopback baseUrl carrying userinfo', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetch(HEALTH);
    createStellarBillClient({ baseUrl: 'http://user:pass@localhost:9000', fetch });
    expect(warn).not.toHaveBeenCalled();
  });

  it('accepts upper-cased loopback hostnames after URL normalisation', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetch(HEALTH);
    createStellarBillClient({ baseUrl: 'http://127.0.0.1:4000', fetch });
    expect(warn).not.toHaveBeenCalled();
  });
});

describe('isLocalhost() - accepted loopback requests keep their documented state', () => {
  it('still performs the request and returns the parsed result', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({ baseUrl: 'http://localhost:3000', fetch });

    const r = await sdk.getHealth();

    expect(warn).not.toHaveBeenCalled();
    expect(r.status).toBe(200);
    expect(r.data?.status).toBe('ok');
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe('http://localhost:3000/api/health');
  });

  it('does not warn once per call and still authenticates a loopback request', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = mockFetch(HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'http://127.0.0.1:8080',
      token: 'local-token',
      fetch,
    });

    await sdk.getHealth();
    await sdk.getHealth();

    expect(warn).not.toHaveBeenCalled();
    expect(calls).toHaveLength(2);
    expect(calls[0]!.headers['authorization']).toBe('Bearer local-token');
    expect(calls[1]!.headers['authorization']).toBe('Bearer local-token');
  });

  it('never reaches the loopback guard for an https baseUrl (no warning either way)', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch } = mockFetch(HEALTH);
    createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    expect(warn).not.toHaveBeenCalled();
  });
});
