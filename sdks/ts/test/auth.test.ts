import { describe, expect, it } from 'vitest';

import { sanitizeToken, TokenHolder } from '../src/index.js';

describe('sanitizeToken', () => {
  it('returns undefined for undefined input', () => {
    expect(sanitizeToken(undefined)).toBeUndefined();
  });
  it('returns undefined for non-string input', () => {
    expect(sanitizeToken(123 as unknown as string)).toBeUndefined();
    expect(sanitizeToken({} as unknown as string)).toBeUndefined();
    expect(sanitizeToken(null as unknown as string)).toBeUndefined();
  });
  it('returns undefined for empty string after trim', () => {
    expect(sanitizeToken('')).toBeUndefined();
    expect(sanitizeToken('   ')).toBeUndefined();
  });

  // `src/auth.ts:37` — `if (trimmed.length === 0) return undefined;`
  // Rejected path: any input that is empty *after* trimming is dropped so the
  // SDK never emits a blank `Authorization` header.
  it('rejects input that is empty after trimming (auth.ts:37 true path)', () => {
    expect(sanitizeToken('')).toBeUndefined();
    expect(sanitizeToken(' ')).toBeUndefined();
    expect(sanitizeToken('\t')).toBeUndefined();
    expect(sanitizeToken('\n')).toBeUndefined();
    expect(sanitizeToken('\r\n')).toBeUndefined();
    expect(sanitizeToken('  \t \n ')).toBeUndefined();
  });

  // Accepted path: a representative valid token survives trimming and is
  // returned verbatim (the documented `string` result of the branch).
  it('accepts a representative valid token after trimming (auth.ts:37 false path)', () => {
    expect(sanitizeToken('sb_live_abc123')).toBe('sb_live_abc123');
    expect(sanitizeToken('  sb_live_abc123  ')).toBe('sb_live_abc123');
    expect(sanitizeToken('\tsb_live_abc123\n')).toBe('sb_live_abc123');
  });
  it('returns undefined when trimmed string still contains whitespace', () => {
    expect(sanitizeToken('a b')).toBeUndefined();
    expect(sanitizeToken('a b c')).toBeUndefined();
  });
  it('returns trimmed token when surrounding whitespace is stripped', () => {
    // sanitizeToken first trims, then rejects strings still containing whitespace.
    expect(sanitizeToken('  abc  ')).toBe('abc');
    expect(sanitizeToken('\tabc\n')).toBe('abc');
    expect(sanitizeToken('abc')).toBe('abc');
  });
});

// `src/auth.ts:25` — `return typeof this.#token === 'string' && this.#token.length > 0;`
//
// `TokenHolder` owns only one boundary: it reports whether it is holding a
// *non-empty string*. Trimming and whitespace rejection live in
// `sanitizeToken`, so the holder does not trim. The branch is therefore exact:
// `undefined`, non-string values (possible for untyped JS callers) and `''`
// are "no token"; any string of length >= 1 is "has token". These tests pin
// that boundary so it cannot silently drift.
describe('TokenHolder.hasToken boundary (auth.ts:25)', () => {
  it('reports no token for a holder with no initial value', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('reports no token for undefined from both the constructor and set()', () => {
    expect(new TokenHolder(undefined).hasToken()).toBe(false);

    const holder = new TokenHolder('live');
    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('uses length === 0 vs length === 1 as the exact boundary', () => {
    expect(new TokenHolder('').hasToken()).toBe(false);
    expect(new TokenHolder('x').hasToken()).toBe(true);
    // The holder never trims; sanitization is `sanitizeToken`'s job, so a
    // whitespace-only string is still a non-empty token here.
    expect(new TokenHolder('  ').hasToken()).toBe(true);
  });

  it('flips deterministically as tokens rotate across the boundary', () => {
    const holder = new TokenHolder('');
    expect(holder.hasToken()).toBe(false);

    holder.set('x');
    expect(holder.hasToken()).toBe(true);

    holder.set('');
    expect(holder.hasToken()).toBe(false);

    holder.set('sb_live_abc123');
    expect(holder.get()).toBe('sb_live_abc123');
    expect(holder.hasToken()).toBe(true);
  });

  it('reports no token for non-string values without throwing (typeof guard)', () => {
    const nonStrings: unknown[] = [123, 0, null, undefined, {}, [], true, Symbol('s'), () => {}];
    for (const value of nonStrings) {
      expect(new TokenHolder(value as string).hasToken()).toBe(false);
    }

    const holder = new TokenHolder('live');
    for (const value of nonStrings) {
      holder.set(value as string);
      expect(holder.hasToken()).toBe(false);
    }
  });

  it('exposes the stored value verbatim so callers see exactly what was set', () => {
    const holder = new TokenHolder('  padded  ');
    expect(holder.get()).toBe('  padded  ');
    expect(holder.hasToken()).toBe(true);

    holder.set('');
    expect(holder.get()).toBe('');
    expect(holder.hasToken()).toBe(false);
  });
});
