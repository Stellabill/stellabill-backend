/**
 * Focused coverage for the `providedFetch` resolution in `createStellarBillClient`
 * (`sdks/ts/src/client.ts`):
 *
 * ```ts
 * const providedFetch = options.fetch ?? (globalThis.fetch as FetchLike | undefined);
 * if (typeof providedFetch !== 'function') {
 *   throw new StellarBillConfigError('No fetch implementation available. ...');
 * }
 * ```
 *
 * The existing suite only covers the "no global fetch at all" case. These tests
 * pin down the *shape* of the rejection instead: the guard must run on the
 * resolved value (not on the raw option), must be synchronous, must not fall
 * back to a broken global when an explicit `options.fetch` was supplied, and
 * must reject non-callable inputs that a naive truthiness check would accept.
 *
 * `??` (not `||`) is load-bearing here: `options.fetch: 0` / `''` / `false` are
 * non-nullish, so they must reach the `typeof !== 'function'` branch rather than
 * silently falling through to the global fetch.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

const BASE_URL = 'https://api.example.com';

type FetchCall = {
  url: string;
  headers: Record<string, string>;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

/** A fetch double that records every call and answers with a fresh JSON Response. */
function recordingFetch(body: unknown, status = 200): {
  fetch: typeof globalThis.fetch;
  calls: FetchCall[];
  spy: ReturnType<typeof vi.fn>;
} {
  const calls: FetchCall[] = [];
  const spy = vi.fn(async (input: Parameters<typeof fetch>[0]) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ url: toUrl(input), headers });
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: spy as unknown as typeof globalThis.fetch, calls, spy };
}

const realFetch = globalThis.fetch;

function setGlobalFetch(value: unknown): void {
  (globalThis as { fetch?: unknown }).fetch = value;
}

function removeGlobalFetch(): void {
  delete (globalThis as { fetch?: unknown }).fetch;
}

beforeEach(() => {
  setGlobalFetch(realFetch);
});

afterEach(() => {
  setGlobalFetch(realFetch);
  vi.restoreAllMocks();
});

describe('createStellarBillClient - providedFetch resolution', () => {
  it('throws StellarBillConfigError when neither options.fetch nor a global fetch exists', () => {
    removeGlobalFetch();

    expect(() => createStellarBillClient({ baseUrl: BASE_URL })).toThrow(StellarBillConfigError);
    expect(() => createStellarBillClient({ baseUrl: BASE_URL })).toThrow(
      /No fetch implementation available/,
    );
    expect(() => createStellarBillClient({ baseUrl: BASE_URL })).toThrow(/options\.fetch/);
  });

  it('falls back to the global fetch when options.fetch is explicitly undefined or null', async () => {
    const { fetch, calls } = recordingFetch({ status: 'ok' });
    setGlobalFetch(fetch);

    const fromUndefined = createStellarBillClient({ baseUrl: BASE_URL, fetch: undefined });
    const fromNull = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch: null as unknown as typeof fetch,
    });

    await expect(fromUndefined.getHealth()).resolves.toMatchObject({ status: 200 });
    await expect(fromNull.getHealth()).resolves.toMatchObject({ status: 200 });
    expect(calls).toHaveLength(2);
  });

  it('rejects nullish options.fetch when the global fetch is also missing', () => {
    removeGlobalFetch();

    expect(() => createStellarBillClient({ baseUrl: BASE_URL, fetch: undefined })).toThrow(
      StellarBillConfigError,
    );
    expect(() =>
      createStellarBillClient({ baseUrl: BASE_URL, fetch: null as unknown as typeof fetch }),
    ).toThrow(StellarBillConfigError);
  });

  it.each([
    ['a number', 0],
    ['a second number', 42],
    ['an empty string', ''],
    ['a url string', 'https://api.example.com/fetch'],
    ['false', false],
    ['true', true],
    ['a plain object', { call: () => undefined }],
    ['an array', []],
  ])('rejects %s as options.fetch even when a working global fetch exists', (_label, bad) => {
    const { fetch, spy } = recordingFetch({ status: 'ok' });
    setGlobalFetch(fetch);

    expect(() =>
      createStellarBillClient({ baseUrl: BASE_URL, fetch: bad as unknown as typeof fetch }),
    ).toThrow(StellarBillConfigError);

    // A rejected configuration must not silently fall back to the global fetch.
    expect(spy).not.toHaveBeenCalled();
  });

  it('throws synchronously instead of returning a rejected promise', () => {
    removeGlobalFetch();

    let thrown: unknown;
    let returned: unknown;
    try {
      returned = createStellarBillClient({ baseUrl: BASE_URL });
    } catch (err) {
      thrown = err;
    }

    expect(returned).toBeUndefined();
    expect(thrown).toBeInstanceOf(StellarBillConfigError);
    expect((thrown as StellarBillConfigError).name).toBe('StellarBillConfigError');
  });

  it('rejects the configuration before any request is attempted', () => {
    const { fetch, spy } = recordingFetch({ status: 'ok' });
    setGlobalFetch(fetch);

    expect(() =>
      createStellarBillClient({ baseUrl: BASE_URL, fetch: 'not-a-function' as unknown as typeof fetch }),
    ).toThrow(StellarBillConfigError);

    expect(spy).not.toHaveBeenCalled();
  });

  it('prefers an explicit options.fetch over a broken global fetch', async () => {
    const brokenGlobal = vi.fn(() => {
      throw new Error('global fetch must not be consulted');
    });
    setGlobalFetch(brokenGlobal as unknown as typeof globalThis.fetch);

    const { fetch, calls } = recordingFetch({ status: 'ok' });
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(brokenGlobal).not.toHaveBeenCalled();
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe(`${BASE_URL}/api/health`);
  });

  it('surfaces a caller fetch rejection unchanged', async () => {
    const boom = new Error('socket hang up');
    const failing = vi.fn(async () => {
      throw boom;
    });
    const sdk = createStellarBillClient({
      baseUrl: BASE_URL,
      fetch: failing as unknown as typeof globalThis.fetch,
    });

    await expect(sdk.getHealth()).rejects.toBe(boom);
  });

  it('validates baseUrl before resolving fetch, so misconfiguration reports the URL', async () => {
    removeGlobalFetch();

    // baseUrl validation runs first: the missing-fetch guard must not mask it.
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url' })).toThrow(/not a valid URL/);
  });

  it('accepts a fetch-like arrow function bound to a custom recorder', async () => {
    const seen: string[] = [];
    const custom = (async (input: Parameters<typeof fetch>[0]) => {
      seen.push(toUrl(input));
      return new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }) as unknown as typeof globalThis.fetch;

    removeGlobalFetch();
    const sdk = createStellarBillClient({ baseUrl: BASE_URL, fetch: custom });

    await expect(sdk.getHealth()).resolves.toMatchObject({ status: 200 });
    expect(seen).toEqual([`${BASE_URL}/api/health`]);
  });
});
