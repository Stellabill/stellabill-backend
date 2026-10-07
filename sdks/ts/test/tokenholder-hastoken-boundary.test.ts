import { afterEach, describe, expect, it, vi } from 'vitest';

import { TokenHolder, createStellarBillClient } from '../src/index.js';

/**
 * Focused boundary coverage for the `TokenHolder.hasToken()` control-flow
 * branch at `sdks/ts/src/auth.ts:25`:
 *
 *   hasToken(): boolean {
 *     return typeof this.#token === 'string' && this.#token.length > 0;
 *   }
 *
 * The expression is a two-term short-circuit `&&`, so it exposes two
 * observable boundaries that callers (and the auth middleware) depend on:
 *
 *   1. `typeof this.#token === 'string'` — a type boundary. Every non-string
 *      value (`undefined`, `null`, numbers, booleans, objects, arrays,
 *      symbols, functions, boxed `String` objects) must evaluate `false`
 *      *without* evaluating `this.#token.length`, so it can never throw.
 *
 *   2. `this.#token.length > 0` — a length boundary. The empty string is the
 *      only string that fails here; a length-one string and a whitespace-only
 *      string both pass and return `true` (hasToken performs no trimming).
 *
 * These tests drive the branch both directly through `TokenHolder` and, in the
 * final block, through the public SDK surface (`setToken()` →
 * `Authorization` header) so the branch's effect on the request is observable.
 * Existing suites cover the "happy path" and "rejected token" cases; this file
 * pins the exact edges so a future refactor cannot silently flip them.
 */

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// 1. Type boundary — the `typeof this.#token === 'string'` term.
//    Anything that is not a primitive string must short-circuit to `false`
//    before `this.#token.length` is ever read.
// ---------------------------------------------------------------------------

describe('TokenHolder.hasToken() - type boundary (auth.ts:25)', () => {
  it('returns false for undefined (never set and explicitly set)', () => {
    expect(new TokenHolder().hasToken()).toBe(false);
    expect(new TokenHolder(undefined).hasToken()).toBe(false);
  });

  it('returns false for null (typeof null is "object")', () => {
    const holder = new TokenHolder(null as unknown as string);
    expect(holder.hasToken()).toBe(false);
  });

  it.each<[string, unknown]>([
    ['number 0', 0],
    ['number 42', 42],
    ['number -1', -1],
    ['NaN', Number.NaN],
    ['Infinity', Number.POSITIVE_INFINITY],
    ['boolean false', false],
    ['boolean true', true],
    ['plain object', {}],
    ['array', []],
    ['non-empty array', ['a']],
    ['symbol', Symbol('token')],
    ['function', () => 'token'],
    ['bigint', 1n],
  ])('returns false for non-string value (%s) without throwing', (_label, value) => {
    const holder = new TokenHolder(value as unknown as string);
    // The `typeof` guard short-circuits, so `.length` is never touched and the
    // call must not throw for values that lack a `length` property.
    expect(() => holder.hasToken()).not.toThrow();
    expect(holder.hasToken()).toBe(false);
  });

  it('returns false for a boxed String object (typeof object, not string)', () => {
    const holder = new TokenHolder(new String('abc') as unknown as string);
    expect(holder.hasToken()).toBe(false);
    // get() still echoes the stored value verbatim; only hasToken() classifies
    // it as absent because the runtime type is not a primitive string.
    expect(typeof holder.get()).toBe('object');
  });
});

// ---------------------------------------------------------------------------
// 2. Length boundary — the `this.#token.length > 0` term for primitive
//    strings. `''` is the sole rejecting case; length-one and whitespace-only
//    values are accepted because hasToken() does not trim or validate.
// ---------------------------------------------------------------------------

describe('TokenHolder.hasToken() - length boundary (auth.ts:25)', () => {
  it('returns false for the empty string (length === 0)', () => {
    expect(new TokenHolder('').hasToken()).toBe(false);
  });

  it('returns true for the length-one string (smallest accepted value)', () => {
    expect(new TokenHolder('a').hasToken()).toBe(true);
    expect(new TokenHolder('0').hasToken()).toBe(true);
  });

  it('returns true for a normal non-empty token', () => {
    expect(new TokenHolder('valid-token').hasToken()).toBe(true);
    expect(new TokenHolder('a-b-c_123.456').hasToken()).toBe(true);
  });

  it('returns true for whitespace-only strings (hasToken does not trim)', () => {
    // hasToken() only checks `length > 0`, so whitespace passes even though
    // sanitizeToken() would reject it. Pinned so the raw holder contract is
    // deterministic and distinct from the sanitized client contract.
    expect(new TokenHolder(' ').hasToken()).toBe(true);
    expect(new TokenHolder('   ').hasToken()).toBe(true);
    expect(new TokenHolder('\t\n').hasToken()).toBe(true);
  });

  it('flips across the length boundary as the holder is mutated', () => {
    const holder = new TokenHolder('');
    expect(holder.hasToken()).toBe(false);

    holder.set('x');
    expect(holder.hasToken()).toBe(true);

    holder.set('');
    expect(holder.hasToken()).toBe(false);

    holder.set('rotated');
    expect(holder.hasToken()).toBe(true);

    holder.set(undefined);
    expect(holder.hasToken()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// 3. Determinism & contract stability — repeated calls agree, the accessor is
//    side-effect free, and hasToken() stays consistent with get().
// ---------------------------------------------------------------------------

describe('TokenHolder.hasToken() - determinism and contract stability', () => {
  it('returns the same value on every repeated call (no side effects)', () => {
    const present = new TokenHolder('stable');
    const absent = new TokenHolder('');

    const firstPresent = present.hasToken();
    const firstAbsent = absent.hasToken();
    for (let i = 0; i < 100; i += 1) {
      expect(present.hasToken()).toBe(firstPresent);
      expect(absent.hasToken()).toBe(firstAbsent);
    }
    // Reading presence never mutates the stored token.
    expect(present.get()).toBe('stable');
    expect(absent.get()).toBe('');
  });

  it('never throws and always returns a boolean for arbitrary input', () => {
    const values: unknown[] = [undefined, '', '   ', 'a', 0, false, null, {}, [], Symbol('s')];
    for (const value of values) {
      const holder = new TokenHolder(value as string);
      let result: unknown = 'sentinel';
      expect(() => {
        result = holder.hasToken();
      }).not.toThrow();
      expect(typeof result).toBe('boolean');
    }
  });

  it('is true if and only if get() is a non-empty primitive string', () => {
    const samples: unknown[] = [undefined, null, '', ' ', 'x', 'token', 0, 1, false, {}, [], Symbol('s')];
    for (const sample of samples) {
      const holder = new TokenHolder(sample as string);
      const value = holder.get();
      const expected = typeof value === 'string' && value.length > 0;
      // This mirrors the branch itself, guarding the invariant that hasToken()
      // and get() cannot drift apart.
      expect(holder.hasToken()).toBe(expected);
    }
  });
});

// ---------------------------------------------------------------------------
// 4. Observable request behavior — hasToken() is the guard the auth middleware
//    uses before writing the bearer token, so its boolean result must map
//    exactly onto the presence/absence of the `Authorization` header.
// ---------------------------------------------------------------------------

type FetchCall = { headers: Record<string, string> };

function mockFetch(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ headers });
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch, calls };
}

describe('TokenHolder.hasToken() - observable Authorization header behavior', () => {
  it('emits Authorization exactly when hasToken() is true and omits it when false', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const holder = new TokenHolder();

    const transitions: Array<[string, string | undefined, boolean]> = [
      ['absent', undefined, false],
      ['present', 'live-token', true],
      ['cleared', '', false],
      ['present again', 'rotated', true],
    ];

    for (const [, token, expectedHasToken] of transitions) {
      holder.set(token);
      sdk.setToken(token);
      expect(holder.hasToken()).toBe(expectedHasToken);
      await sdk.getHealth();
    }

    expect(calls).toHaveLength(transitions.length);
    expect(calls[0]!.headers['authorization']).toBeUndefined();
    expect(calls[1]!.headers['authorization']).toBe('Bearer live-token');
    expect(calls[2]!.headers['authorization']).toBeUndefined();
    expect(calls[3]!.headers['authorization']).toBe('Bearer rotated');
  });
});
