import { describe, expect, it } from 'vitest';

import { TokenHolder, sanitizeToken } from '../src/index.js';

describe('TokenHolder', () => {
  describe('hasToken', () => {
    it('returns true for accepted token input', () => {
      const holder = new TokenHolder('valid-token');
      expect(holder.hasToken()).toBe(true);
      expect(holder.get()).toBe('valid-token');

      holder.set('another-token');
      expect(holder.hasToken()).toBe(true);
      expect(holder.get()).toBe('another-token');
    });
  });
});

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
