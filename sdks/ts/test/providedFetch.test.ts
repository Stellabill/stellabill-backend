/**
 * Focused tests for the `providedFetch` resolution branch in client.ts:155.
 *
 * The branch under test:
 *   const providedFetch = options.fetch ?? (globalThis.fetch as FetchLike | undefined);
 *   if (typeof providedFetch !== 'function') {
 *     throw new StellarBillConfigError('No fetch implementation available...');
 *   }
 *
 * Contract:
 *   - Any callable function supplied via `options.fetch` is accepted and used.
 *   - Non-function values (string, number, object, null, boolean, array) throw
 *     StellarBillConfigError regardless of globalThis.fetch.
 *   - When `options.fetch` is omitted, the client falls back to `globalThis.fetch`.
 *   - When both `options.fetch` and `globalThis.fetch` are present, `options.fetch`
 *     takes priority (the global is never called).
 *   - When neither is available the client throws at construction time, before any
 *     network activity.
 *
 * Test structure deliberately mirrors client.test.ts conventions:
 *   - `mockFetchOnce` helper to build a spy fetch that records calls.
 *   - `afterEach(vi.restoreAllMocks)` to clean up spies.
 *   - All assertions on `StellarBillConfigError` use the exported class so the
 *     check is structural rather than string-based.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, type FetchLike, StellarBillConfigError } from '../src/index.js';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type FetchCall = { url: string };

/**
 * Minimal mock fetch that records every call and always returns a 200 JSON
 * response with the supplied body.
 */
function mockFetch(
  body: unknown = { status: 'ok', service: 'stellarbill-backend' },
  statusCode = 200,
): { fetch: FetchLike; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const text = JSON.stringify(body);
  const impl: FetchLike = vi.fn(async (input) => {
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : (input as Request).url;
    calls.push({ url });
    return new Response(text, {
      status: statusCode,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: impl, calls };
}

/** Temporarily replace globalThis.fetch and restore it in afterEach. */
function patchGlobalFetch(value: unknown): void {
  const g = globalThis as Record<string, unknown>;
  const original = g['fetch'];
  g['fetch'] = value;
  // Register cleanup so each test stays isolated.
  afterEach(() => {
    g['fetch'] = original;
  });
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

afterEach(() => {
  vi.restoreAllMocks();
});

describe('providedFetch branch — happy path: options.fetch is a function', () => {
  it('accepts a standard async fetch function and uses it for requests', async () => {
    const { fetch, calls } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.getHealth();

    // The mock fetch was called exactly once.
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toContain('/api/health');
    // The SDK properly unwrapped the response.
    expect(result.status).toBe(200);
    expect(result.data?.status).toBe('ok');
    expect(result.error).toBeUndefined();
  });

  it('calls the provided fetch function on every subsequent request', async () => {
    const { fetch, calls } = mockFetch({ subscriptions: [], pagination: { has_more: false } });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await sdk.listSubscriptions();
    await sdk.listSubscriptions();

    expect(calls).toHaveLength(2);
  });

  it('returns the correct requestMethod and requestUrl from the fetch call', async () => {
    const { fetch } = mockFetch({ status: 'ok', service: 'stellarbill-backend' });
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.getHealth();

    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
  });

  it('uses options.fetch when globalThis.fetch is also a valid function', async () => {
    const { fetch: optionsFetch, calls: optionsCalls } = mockFetch({
      status: 'ok',
      service: 'stellarbill-backend',
    });
    const { fetch: globalFetch, calls: globalCalls } = mockFetch({
      status: 'ok',
      service: 'stellarbill-backend',
    });

    // Temporarily install an alternative global fetch.
    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = globalFetch;
    try {
      const sdk = createStellarBillClient({
        baseUrl: 'https://api.example.com',
        fetch: optionsFetch,
      });
      await sdk.getHealth();
    } finally {
      g['fetch'] = original;
    }

    // options.fetch took priority.
    expect(optionsCalls).toHaveLength(1);
    // globalThis.fetch was never called.
    expect(globalCalls).toHaveLength(0);
  });

  it('accepts a fetch implementation that returns a non-200 status', async () => {
    const { fetch } = mockFetch({ error: 'Not Found', message: 'gone', code: 'missing' }, 404);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(404);
    expect(result.error?.code).toBe('missing');
  });

  it('accepts a minimal arrow function as fetch', async () => {
    // Arrow functions satisfy `typeof x === "function"`.
    const arrowFetch: FetchLike = async () =>
      new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });

    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: arrowFetch });
    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
  });
});

describe('providedFetch branch — globalThis.fetch fallback', () => {
  it('falls back to globalThis.fetch when options.fetch is omitted', async () => {
    const { fetch: globalFetch, calls } = mockFetch({
      status: 'ok',
      service: 'stellarbill-backend',
    });

    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = globalFetch;
    try {
      // No fetch option supplied.
      const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com' });
      await sdk.getHealth();
    } finally {
      g['fetch'] = original;
    }

    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toContain('/api/health');
  });

  it('falls back to globalThis.fetch when options.fetch is explicitly undefined', async () => {
    const { fetch: globalFetch, calls } = mockFetch({
      status: 'ok',
      service: 'stellarbill-backend',
    });

    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = globalFetch;
    try {
      const sdk = createStellarBillClient({
        baseUrl: 'https://api.example.com',
        fetch: undefined,
      });
      await sdk.getHealth();
    } finally {
      g['fetch'] = original;
    }

    expect(calls).toHaveLength(1);
  });
});

describe('providedFetch branch — error path: non-function values throw at construction', () => {
  /**
   * Helper: attempt to construct a client with a non-function fetch value and
   * assert that a StellarBillConfigError is thrown.  The globalThis.fetch is
   * cleared so the ?? fallback cannot rescue a bad explicit value — the branch
   * only falls back when options.fetch is absent (undefined via ??).
   *
   * NOTE: The ?? operator only falls back when the left-hand side is `null` or
   * `undefined`.  All other falsy or non-function values (e.g. 0, '', false,
   * an object) propagate through to the typeof check, which then throws.
   */
  function expectConstructionThrows(fetchValue: unknown): void {
    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = undefined; // ensure no global rescue
    try {
      expect(() =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: fetchValue as FetchLike,
        }),
      ).toThrow(StellarBillConfigError);
    } finally {
      g['fetch'] = original;
    }
  }

  it('throws StellarBillConfigError when options.fetch is a string', () => {
    expectConstructionThrows('https://api.example.com/fetch');
  });

  it('throws StellarBillConfigError when options.fetch is a number', () => {
    expectConstructionThrows(42);
  });

  it('throws StellarBillConfigError when options.fetch is zero', () => {
    expectConstructionThrows(0);
  });

  it('throws StellarBillConfigError when options.fetch is a plain object', () => {
    expectConstructionThrows({ then: 'not-a-function' });
  });

  it('throws StellarBillConfigError when options.fetch is an empty object', () => {
    expectConstructionThrows({});
  });

  it('throws StellarBillConfigError when options.fetch is an array', () => {
    expectConstructionThrows([]);
  });

  it('throws StellarBillConfigError when options.fetch is a boolean true', () => {
    expectConstructionThrows(true);
  });

  it('throws StellarBillConfigError when options.fetch is a boolean false', () => {
    expectConstructionThrows(false);
  });

  it('throws StellarBillConfigError when options.fetch is null (no global fallback)', () => {
    // null is handled by ??; it falls through to globalThis.fetch which is also
    // undefined here, so providedFetch ends up undefined → throws.
    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = undefined;
    try {
      expect(() =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: null as unknown as FetchLike,
        }),
      ).toThrow(StellarBillConfigError);
    } finally {
      g['fetch'] = original;
    }
  });

  it('error message contains fetch-related guidance', () => {
    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = undefined;
    try {
      expect(() =>
        createStellarBillClient({
          baseUrl: 'https://api.example.com',
          fetch: 'not-a-function' as unknown as FetchLike,
        }),
      ).toThrow(/fetch/i);
    } finally {
      g['fetch'] = original;
    }
  });

  it('error is a StellarBillConfigError instance (subtype check)', () => {
    const g = globalThis as Record<string, unknown>;
    const original = g['fetch'];
    g['fetch'] = undefined;
    let thrown: unknown;
    try {
      createStellarBillClient({
        baseUrl: 'https://api.example.com',
        fetch: 'bad' as unknown as FetchLike,
      });
    } catch (e) {
      thrown = e;
    } finally {
      g['fetch'] = original;
    }

    expect(thrown).toBeInstanceOf(StellarBillConfigError);
    // StellarBillConfigError must also be an Error.
    expect(thrown).toBeInstanceOf(Error);
  });
});

describe('providedFetch branch — error path: no fetch available at all', () => {
  beforeEach(() => {
    // Suppress globalThis.fetch for the entire sub-suite.
    patchGlobalFetch(undefined);
  });

  it('throws when both options.fetch is absent and globalThis.fetch is undefined', () => {
    expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com' })).toThrow(
      StellarBillConfigError,
    );
  });

  it('throws when options.fetch is absent and globalThis.fetch is null', () => {
    (globalThis as Record<string, unknown>)['fetch'] = null;
    expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com' })).toThrow(
      StellarBillConfigError,
    );
  });

  it('does NOT throw when a valid options.fetch is supplied despite missing global', () => {
    const { fetch } = mockFetch();
    expect(() =>
      createStellarBillClient({ baseUrl: 'https://api.example.com', fetch }),
    ).not.toThrow();
  });

  it('throws before any network activity occurs (no calls recorded)', () => {
    const { fetch, calls } = mockFetch();
    // Swap out the options.fetch with an object after construction attempt.
    try {
      createStellarBillClient({
        baseUrl: 'https://api.example.com',
        // Intentionally bad value — construction must throw synchronously.
        fetch: {} as unknown as FetchLike,
      });
    } catch {
      // Expected.
    }
    // The mock fetch was never invoked because the constructor threw.
    expect(calls).toHaveLength(0);
    void fetch; // keep reference alive
  });
});
