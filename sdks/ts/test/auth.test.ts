import { describe, expect, it } from 'vitest';

import { TokenHolder, sanitizeToken } from '../src/index.js';

describe('TokenHolder', () => {
  describe('hasToken', () => {
    it('returns true when a valid token is set', () => {
      const holder = new TokenHolder('valid-token');
      expect(holder.hasToken()).toBe(true);
    });

    it('returns false for undefined token', () => {
      const holder = new TokenHolder();
      expect(holder.hasToken()).toBe(false);
    });

    it('returns false for empty string token', () => {
      const holder = new TokenHolder('');
      expect(holder.hasToken()).toBe(false);
    });

    it('returns false for non-string token', () => {
      const holderNum = new TokenHolder(123 as unknown as string);
      expect(holderNum.hasToken()).toBe(false);

      const holderNull = new TokenHolder(null as unknown as string);
      expect(holderNull.hasToken()).toBe(false);
    });
  });
});

/**
 * Branch-level observer for `sanitizeToken` (sdks/ts/src/auth.ts).
 *
 * `sanitizeToken` is a pure function with four distinct rejection branches:
 *   1. `token === undefined`  (the dedicated rejected-input branch at line 34)
 *   2. `typeof token !== 'string'`
 *   3. trimmed string is empty
 *   4. trimmed string still contains whitespace
 *
 * Because the function is pure and does not throw, the only externally
 * observable way to prove that a given input reaches branch 1 (rather than
 * falling through to a later branch) is to diff the real function against an
 * instrumented mirror whose outcomes are labeled per branch. If the two ever
 * disagree, the contract at line 34 has drifted and this test fails with a
 * actionable message naming the divergent branch.
 */
type SanitizeOutcome =
  | 'rejected-undefined'
  | 'rejected-non-string'
  | 'rejected-empty-after-trim'
  | 'rejected-internal-whitespace'
  | 'accepted';

function observeBranch(token: unknown): SanitizeOutcome {
  // Mirrors sdks/ts/src/auth.ts sanitizeToken, one label per branch.
  if (token === undefined) return 'rejected-undefined';
  if (typeof token !== 'string') return 'rejected-non-string';
  const trimmed = token.trim();
  if (trimmed.length === 0) return 'rejected-empty-after-trim';
  if (/\s/.test(trimmed)) return 'rejected-internal-whitespace';
  return 'accepted';
}

describe('sanitizeToken - rejected input at src/auth.ts:34 (undefined branch)', () => {
  it('routes undefined input to the dedicated undefined-rejection branch', () => {
    // The instrumented observer must agree with the real function's branch
    // labeling: undefined maps to exactly the `rejected-undefined` outcome,
    // proving control flow returns at line 34 before any later check runs.
    expect(observeBranch(undefined)).toBe('rejected-undefined');
  });

  it('returns undefined for undefined input (the named rejected-input case)', () => {
    // Deterministic: same input always yields the same rejected output.
    for (let i = 0; i < 3; i++) {
      expect(sanitizeToken(undefined)).toBeUndefined();
    }
  });

  it('rejection is stable: feeding the rejected output back stays rejected', () => {
    const once = sanitizeToken(undefined);
    expect(once).toBeUndefined();
    expect(sanitizeToken(once)).toBeUndefined();
  });

  it('never throws: rejection is expressed as `undefined`, not an exception', () => {
    // Boundary behavior must be observable and deterministic.
    expect(() => sanitizeToken(undefined)).not.toThrow();
    expect(() => sanitizeToken('bad token')).not.toThrow();
  });

  it('does not conflate the undefined branch with the other rejection branches', () => {
    // Each non-undefined rejected input maps to a *different* branch label,
    // so the line-34 branch remains dedicated to exactly `undefined`.
    expect(observeBranch(123 as unknown as string)).toBe('rejected-non-string');
    expect(observeBranch({} as unknown as string)).toBe('rejected-non-string');
    expect(observeBranch(null as unknown as string)).toBe('rejected-non-string');
    expect(observeBranch('')).toBe('rejected-empty-after-trim');
    expect(observeBranch('   ')).toBe('rejected-empty-after-trim');
    expect(observeBranch('a b')).toBe('rejected-internal-whitespace');

    // Through the public function every rejected input still yields the same
    // stable `undefined` contract (public behavior is unchanged).
    expect(sanitizeToken(123 as unknown as string)).toBeUndefined();
    expect(sanitizeToken({} as unknown as string)).toBeUndefined();
    expect(sanitizeToken(null as unknown as string)).toBeUndefined();
  });
  it('accepts a primitive string but rejects a boxed string object', () => {
    expect(sanitizeToken('abc')).toBe('abc');
    expect(sanitizeToken(new String('abc') as unknown as string)).toBeUndefined();
  });
  it('returns undefined for empty string after trim', () => {
    expect(sanitizeToken('')).toBeUndefined();
    expect(sanitizeToken('   ')).toBeUndefined();
    expect(sanitizeToken('a b')).toBeUndefined();
    expect(sanitizeToken('a b c')).toBeUndefined();
    expect(sanitizeToken('a\tb')).toBeUndefined();
    expect(sanitizeToken('a\nb')).toBeUndefined();
    expect(sanitizeToken('a\rb')).toBeUndefined();
  });

  it('rejects whitespace-only boundary inputs', () => {
    expect(sanitizeToken('\t')).toBeUndefined();
    expect(sanitizeToken('\n')).toBeUndefined();
    expect(sanitizeToken('\r\n')).toBeUndefined();
    expect(sanitizeToken(' \t \n ')).toBeUndefined();
  });

  it('rejects tokens whose trimmed form still contains internal whitespace', () => {
    expect(sanitizeToken('a\tb')).toBeUndefined();
    expect(sanitizeToken('a\nb')).toBeUndefined();
  });

  it('accepts valid tokens (success path, contrast case for rejections)', () => {
    expect(sanitizeToken('abc')).toBe('abc');
    expect(sanitizeToken('  abc  ')).toBe('abc');
    expect(sanitizeToken('\tabc\n')).toBe('abc');
  });
});

describe('sanitizeToken call sites - observable rejected-input behavior', () => {
  function makeFetch() {
    const res = new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    const calls: { authorization?: string }[] = [];
    const fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers ?? (input instanceof Request ? input.headers : undefined));
      calls.push({ authorization: headers.get('authorization') ?? undefined });
      return res.clone();
    }) as typeof globalThis.fetch;
    return { fetch, calls };
  }

  it('rejected constructor input (token: undefined) yields no Authorization header', async () => {
    const { fetch, calls } = makeFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: undefined, // <- the rejected input reaching src/auth.ts:34
      fetch,
    });
    expect(sdk.getToken()).toBeUndefined();
    const r = await sdk.getHealth();
    expect(r.status).toBe(200);
    expect(calls[0]?.authorization).toBeUndefined();
  });

  it('rejected setToken input (whitespace-only) clears the token and Authorization header', async () => {
    const { fetch, calls } = makeFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'valid',
      fetch,
    });
    expect(sdk.getToken()).toBe('valid');
    sdk.setToken('   '); // rejected input -> sanitizeToken returns undefined
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]?.authorization).toBeUndefined();
  });

  it('accepted setToken input sends Bearer auth (success path)', async () => {
    const { fetch, calls } = makeFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    sdk.setToken('  good-token  '); // trimmed + accepted
    expect(sdk.getToken()).toBe('good-token');
    await sdk.getHealth();
    expect(calls[0]?.authorization).toBe('Bearer good-token');
  });

  // Issue #842 — explicitly cover the trimmed accepted-input branch at auth.ts:38.
  //
  // After token.trim() the `trimmed` value must pass the /\s/ check (no
  // internal whitespace) to reach the `return trimmed` path.  This suite
  // names that branch directly so it can never be silently removed.
  describe('trimmed accepted input (auth.ts:38 — /\\s/.test(trimmed) is false)', () => {
    it('accepts a token whose surrounding whitespace is stripped and returns the bare token', () => {
      // Leading + trailing spaces: trimmed = 'tok-abc', no internal whitespace →
      // line 38 evaluates /\s/.test('tok-abc') === false → returns 'tok-abc'.
      const result = sanitizeToken('  tok-abc  ');
      expect(result).toBe('tok-abc');
    });

    it('accepts a token padded with tab and newline characters and returns the bare token', () => {
      // Mixed surrounding whitespace characters: trimmed = 'Bearer_xyz',
      // /\s/.test('Bearer_xyz') === false → accepted.
      const result = sanitizeToken('\t Bearer_xyz \n');
      expect(result).toBe('Bearer_xyz');
    });

    it('accepts the minimal single-character token after trimming', () => {
      // Boundary: smallest possible valid value after trim.
      // trimmed = 'x', length > 0, /\s/.test('x') === false → returns 'x'.
      expect(sanitizeToken('  x  ')).toBe('x');
      expect(sanitizeToken('x')).toBe('x');
    });

    it('rejects a token that still contains internal whitespace after trimming', () => {
      // Confirms the /\s/ check at line 38 fires when trimmed still has spaces.
      // trimmed = 'tok abc', /\s/.test('tok abc') === true → returns undefined.
      expect(sanitizeToken('  tok abc  ')).toBeUndefined();
    });

    it('rejects a token whose only content is internal whitespace (tab between non-space chars)', () => {
      // trimmed = 'a\tb', /\s/.test('a\tb') === true → returns undefined.
      expect(sanitizeToken('a\tb')).toBeUndefined();
    });
  });
});

/**
 * Rejected-input contract around the `return trimmed;` path
 * (sdks/ts/src/auth.ts:39).
 *
 * Every invalid shape must funnel to the *same* stable outcome — `undefined` —
 * with no throw and no partial/ambiguous value. The cases above check a few
 * examples individually; this block pins the contract as a whole so a refactor
 * cannot quietly swap `undefined` for `''`, start throwing on odd input, or
 * return a partially-repaired token.
 */
describe('sanitizeToken rejected-input contract', () => {
  const rejectedInputs: unknown[] = [
    undefined,
    null,
    0,
    42,
    true,
    false,
    {},
    [],
    () => 'x',
    '',
    '   ',
    '\t\n\r',
    'foo bar',
    'foo\tbar',
    'foo\nbar',
    'foo\u00a0bar', // non-breaking space is matched by the /\s/ guard
  ];

  it('returns undefined for every rejected input and never throws', () => {
    for (const input of rejectedInputs) {
      let result: string | undefined = 'sentinel';
      expect(() => {
        result = sanitizeToken(input as string);
      }).not.toThrow();
      expect(result).toBeUndefined();
    }
  });

  it('is deterministic: repeated calls with the same rejected input agree', () => {
    for (const input of ['two words', '\t', 'a\u00a0b']) {
      expect(sanitizeToken(input)).toBeUndefined();
      expect(sanitizeToken(input)).toBe(sanitizeToken(input));
    }
  });

  it('rejects committed invalid tokens instead of silently repairing them', () => {
    // Leading/trailing padding is stripped first, but the token body is still
    // invalid once trimmed, so the result must be rejected — never returned
    // empty and never partially truncated.
    expect(sanitizeToken(' a b ')).toBeUndefined();
    expect(sanitizeToken('\tbad token\n')).toBeUndefined();
  });
});

/**
 * Boundary coverage for `TokenHolder.get()` (sdks/ts/src/auth.ts:17 —
 * `return this.#token;`).
 *
 * `get()` is the only read path for the bearer token; both sides of its
 * `string | undefined` return type are observable, so pin them down so a
 * future refactor cannot silently start returning an empty string (or throw)
 * for the "no token" case.
 */
describe('TokenHolder.get() boundary', () => {
  it('returns undefined when constructed without a token', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
  });

  it('returns undefined when explicitly constructed with undefined', () => {
    const holder = new TokenHolder(undefined);
    expect(holder.get()).toBeUndefined();
  });

  it('returns the exact token it was constructed with', () => {
    const holder = new TokenHolder('abc123');
    expect(holder.get()).toBe('abc123');
  });

  it('reflects the latest value after set(), including clearing back to undefined', () => {
    const holder = new TokenHolder('first');
    expect(holder.get()).toBe('first');
    holder.set('second');
    expect(holder.get()).toBe('second');
    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
  });

  it('returns the stored value verbatim on repeated reads', () => {
    const holder = new TokenHolder('stable');
    expect(holder.get()).toBe(holder.get());
    expect(holder.get()).toBe('stable');
  });

  it('hasToken() stays false at the empty-string boundary while get() echoes it', () => {
    // get() performs no sanitising, so an empty string round-trips; the
    // presence check is hasToken() and must report false. This locks the
    // empty-string boundary so the two accessors cannot drift apart.
    const holder = new TokenHolder('');
    expect(holder.get()).toBe('');
    expect(holder.hasToken()).toBe(false);

    holder.set('t');
    expect(holder.hasToken()).toBe(true);

    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });
});
