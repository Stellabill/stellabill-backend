import { describe, expect, it } from 'vitest';

import { sanitizeToken } from '../src/index.js';

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
