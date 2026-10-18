import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  createStellarBillClient,
  StellarBillConfigError,
  StellarBillError,
  type StellarBillClientOptions,
} from '../src/index.js';

/**
 * Focused boundary tests for `validateBaseUrl` in `sdks/ts/src/client.ts`
 * (issue #859 — the `throw new StellarBillConfigError('baseUrl is required')`
 * branch at ~line 84).
 *
 * `validateBaseUrl` is intentionally module-private, so every assertion below
 * goes through the public entrypoint `createStellarBillClient` and observes
 * either the exact error that escapes or the exact URL the SDK requests. The
 * behaviour is pinned from the real control-flow branches in the source, not
 * guessed:
 *
 *   1. `raw === undefined || raw === null`      -> "baseUrl is required"
 *   2. `typeof raw !== 'string' || blank`       -> "baseUrl must be a non-empty string"
 *   3. `new URL(raw)` throws                    -> `baseUrl "<raw>" is not a valid URL`
 *   4. otherwise                               -> `parsed.toString()` with trailing slashes stripped
 */

/** A fetch double that records every request URL and always answers 200 / health. */
function recordingFetch(): { fetch: typeof globalThis.fetch; calls: string[] } {
  const calls: string[] = [];
  const spy = vi.fn(async (input: RequestInfo | URL) => {
    calls.push(typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url);
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: spy as unknown as typeof globalThis.fetch, calls };
}

/** Invoke `fn` synchronously and return whatever it threw (or `undefined`). */
function thrownBy(fn: () => unknown): unknown {
  try {
    fn();
    return undefined;
  } catch (err) {
    return err;
  }
}

/**
 * Built with a cast on purpose: `baseUrl` is typed as `string`, but callers can
 * (and do) reach the validator with non-string values at runtime, which is
 * exactly the boundary this suite pins down.
 */
function optionsWithBaseUrl(value: unknown, fetch: typeof globalThis.fetch): StellarBillClientOptions {
  return { baseUrl: value as string, fetch };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('createStellarBillClient - baseUrl required (client.ts:84)', () => {
  it('throws StellarBillConfigError "baseUrl is required" when baseUrl is undefined', () => {
    const { fetch, calls } = recordingFetch();
    const err = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(undefined, fetch)));

    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).name).toBe('StellarBillConfigError');
    expect((err as Error).message).toBe('baseUrl is required');
    // The validator runs before the client is built, so no request may escape.
    expect(calls).toHaveLength(0);
  });

  it('throws StellarBillConfigError "baseUrl is required" when baseUrl is null', () => {
    const { fetch, calls } = recordingFetch();
    const err = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(null, fetch)));

    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect((err as Error).message).toBe('baseUrl is required');
    expect(calls).toHaveLength(0);
  });

  it('treats an omitted baseUrl option as the "baseUrl is required" branch', () => {
    const err = thrownBy(() => createStellarBillClient({} as unknown as StellarBillClientOptions));
    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect((err as Error).message).toBe('baseUrl is required');
  });

  it('is distinct from StellarBillError (config error, never an API error)', () => {
    const { fetch } = recordingFetch();
    const err = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(undefined, fetch)));
    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect(err).not.toBeInstanceOf(StellarBillError);
  });

  it('is deterministic: repeated calls produce an identical error type and message', () => {
    const { fetch } = recordingFetch();
    const first = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(undefined, fetch))) as Error;
    const second = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(undefined, fetch))) as Error;

    expect(first.constructor).toBe(StellarBillConfigError);
    expect(second.constructor).toBe(StellarBillConfigError);
    expect(second.name).toBe(first.name);
    expect(second.message).toBe(first.message);
  });

  it('rejects the missing baseUrl before checking fetch availability', () => {
    // Ordering proof: with no usable fetch at all, a bad baseUrl must still
    // surface the baseUrl error (validator runs first), while a valid baseUrl
    // falls through to the "No fetch" error.
    const saved = (globalThis as { fetch?: unknown }).fetch;
    (globalThis as { fetch?: unknown }).fetch = undefined;
    try {
      const badUrl = thrownBy(() => createStellarBillClient({ baseUrl: undefined as unknown as string }));
      expect(badUrl).toBeInstanceOf(StellarBillConfigError);
      expect((badUrl as Error).message).toBe('baseUrl is required');

      const validUrl = thrownBy(() =>
        createStellarBillClient({ baseUrl: 'https://api.example.com' }),
      );
      expect(validUrl).toBeInstanceOf(StellarBillConfigError);
      expect((validUrl as Error).message).toMatch(/No fetch implementation available/);
    } finally {
      (globalThis as { fetch?: unknown }).fetch = saved;
    }
  });
});

describe('createStellarBillClient - baseUrl must be a non-empty string', () => {
  const cases: Array<[string, unknown]> = [
    ['empty string', ''],
    ['single space', ' '],
    ['spaces only', '   '],
    ['tab + newline only', '\t\n'],
    ['carriage return only', '\r'],
    ['number 123', 123],
    ['number 0', 0],
    ['NaN', Number.NaN],
    ['boolean false', false],
    ['plain object', {}],
    ['empty array', []],
    ['String object wrapper', new String('https://api.example.com')],
  ];

  it.each(cases)('rejects %s with "baseUrl must be a non-empty string"', (_label, value) => {
    const { fetch, calls } = recordingFetch();
    const err = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(value, fetch)));

    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect((err as Error).message).toBe('baseUrl must be a non-empty string');
    expect(calls).toHaveLength(0);
  });

  it('does not confuse the empty-string branch with the missing-value branch', () => {
    const empty = thrownBy(() => createStellarBillClient({ baseUrl: '' }));
    const missing = thrownBy(() => createStellarBillClient({ baseUrl: undefined as unknown as string }));
    expect((empty as Error).message).toBe('baseUrl must be a non-empty string');
    expect((missing as Error).message).toBe('baseUrl is required');
    expect((empty as Error).message).not.toBe((missing as Error).message);
  });
});

describe('createStellarBillClient - malformed baseUrl', () => {
  const cases: Array<[string, string]> = [
    ['bare word', 'not-a-url'],
    ['host without scheme', 'example.com'],
    ['protocol-relative URL', '//example.com'],
    ['https scheme only', 'https://'],
    ['http scheme only', 'http://'],
    ['scheme with empty host', 'https://:8080'],
    ['whitespace inside host', 'https://exa mple.com'],
  ];

  it.each(cases)('rejects %s with the exact URL message', (_label, value) => {
    const { fetch, calls } = recordingFetch();
    const err = thrownBy(() => createStellarBillClient(optionsWithBaseUrl(value, fetch)));

    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect((err as Error).message).toBe(`baseUrl "${value}" is not a valid URL`);
    expect(calls).toHaveLength(0);
  });

  it('embeds the offending raw value verbatim in the error message', () => {
    const value = 'ht tp://nope';
    const err = thrownBy(() => createStellarBillClient({ baseUrl: value })) as Error;
    expect(err.message).toBe('baseUrl "ht tp://nope" is not a valid URL');
    expect(err.message).toContain(value);
  });
});

describe('createStellarBillClient - accepted baseUrl normalization', () => {
  const cases: Array<[string, string, string]> = [
    ['no trailing slash', 'https://api.example.com', 'https://api.example.com/api/health'],
    ['single trailing slash', 'https://api.example.com/', 'https://api.example.com/api/health'],
    ['many trailing slashes', 'https://api.example.com///', 'https://api.example.com/api/health'],
    ['path with trailing slash', 'https://api.example.com/v1/', 'https://api.example.com/v1/api/health'],
    ['path without trailing slash', 'https://api.example.com/v1', 'https://api.example.com/v1/api/health'],
    ['uppercase scheme + host', 'HTTPS://API.Example.COM/', 'https://api.example.com/api/health'],
    ['explicit default port', 'https://api.example.com:443', 'https://api.example.com/api/health'],
    ['non-default port', 'https://api.example.com:8443/', 'https://api.example.com:8443/api/health'],
    ['surrounding whitespace', '  https://api.example.com  ', 'https://api.example.com/api/health'],
    ['http localhost', 'http://localhost:8080/', 'http://localhost:8080/api/health'],
  ];

  it.each(cases)('normalizes %s into a single-slash request URL', async (_label, baseUrl, expected) => {
    const { fetch, calls } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl, fetch });
    await sdk.getHealth();

    expect(calls).toHaveLength(1);
    expect(calls[0]).toBe(expected);
    // No doubled separators from an un-stripped baseUrl: the scheme separator is
    // the only `//` in the composed URL. (A literal `'//api'` check would be
    // wrong here — every URL on the `api.` host contains it right after the
    // scheme.)
    const requestUrl = calls[0] ?? '';
    expect(requestUrl.split('//')).toHaveLength(2);
  });

  it('never double-slashes when the baseUrl ends in many slashes', async () => {
    const { fetch, calls } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com//////', fetch });
    await sdk.getHealth();
    expect(calls[0]).toBe('https://api.example.com/api/health');
  });

  it('accepts plain http and warns once that it is insecure, then still issues the request', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const { fetch, calls } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'http://example.com/', fetch });
    const res = await sdk.getHealth();

    expect(res.status).toBe(200);
    expect(calls[0]).toBe('http://example.com/api/health');
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
  });

  it('currently accepts non-http(s) schemes (validateBaseUrl does not restrict the scheme)', async () => {
    // Documented current contract: `new URL()` accepts any absolute scheme, so
    // validateBaseUrl lets e.g. ws:// through. Pinned so a future scheme
    // allow-list change is an explicit, visible decision.
    const { fetch, calls } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'ws://example.com/', fetch });
    await sdk.getHealth();
    expect(calls[0]).toBe('ws://example.com/api/health');
  });
});
