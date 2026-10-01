import { describe, expect, it } from 'vitest';

import { TokenHolder, sanitizeToken } from '../src/index.js';

describe('sanitizeToken', () => {
  it('returns undefined for undefined input', () => {
    expect(sanitizeToken(undefined)).toBeUndefined();
  });
  it('returns undefined for non-string input', () => {
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
  });
  it('returns undefined when trimmed string still contains whitespace', () => {
    expect(sanitizeToken('a b')).toBeUndefined();
    expect(sanitizeToken('a b c')).toBeUndefined();
    expect(sanitizeToken('a\tb')).toBeUndefined();
    expect(sanitizeToken('a\nb')).toBeUndefined();
    expect(sanitizeToken('a\rb')).toBeUndefined();
  });
  it('returns trimmed token when surrounding whitespace is stripped', () => {
    // sanitizeToken first trims, then rejects strings still containing whitespace.
    expect(sanitizeToken('  abc  ')).toBe('abc');
    expect(sanitizeToken('\tabc\n')).toBe('abc');
    expect(sanitizeToken('abc')).toBe('abc');
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

describe('TokenHolder', () => {
  it('initializes with undefined when no token provided', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);

    const holderExplicit = new TokenHolder(undefined);
    expect(holderExplicit.get()).toBeUndefined();
    expect(holderExplicit.hasToken()).toBe(false);
  });

  it('accepts representative valid token and preserves state via get() and hasToken()', () => {
    // Verifies the `return this.#token;` branch accepts representative valid inputs
    // and preserves the exact token string value without alteration.
    const representativeToken = 'sb_live_secret_key_12345';
    const holder = new TokenHolder(representativeToken);
    expect(holder.get()).toBe(representativeToken);
    expect(holder.hasToken()).toBe(true);

    // Also assert various representative valid token formats (hex, uuid, jwt, single char)
    const validTokens = [
      'sb_sec_0123456789abcdef',
      'bearer-token-uuid-1234-5678',
      'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.signature',
      'valid_token_without_whitespace',
      'a',
    ];
    for (const token of validTokens) {
      const h = new TokenHolder(token);
      expect(h.get()).toBe(token);
      expect(h.hasToken()).toBe(true);
    }
  });

  it('accepts valid token on set() and preserves updated token and state', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);

    const firstToken = 'sb_live_initial_token_111';
    holder.set(firstToken);
    expect(holder.get()).toBe(firstToken);
    expect(holder.hasToken()).toBe(true);

    const secondToken = 'sb_live_rotated_token_222';
    holder.set(secondToken);
    expect(holder.get()).toBe(secondToken);
    expect(holder.hasToken()).toBe(true);
  });

  it('rotates token value and updates hasToken state for empty and undefined boundaries', () => {
    const holder = new TokenHolder('initial_token');
    expect(holder.get()).toBe('initial_token');
    expect(holder.hasToken()).toBe(true);

    holder.set('rotated_token');
    expect(holder.get()).toBe('rotated_token');
    expect(holder.hasToken()).toBe(true);

    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);

    holder.set('');
    expect(holder.get()).toBe('');
    expect(holder.hasToken()).toBe(false);
  });
});

