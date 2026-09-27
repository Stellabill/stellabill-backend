import { describe, expect, it } from 'vitest';

import { TokenHolder, sanitizeToken } from '../src/index.js';

describe('sanitizeToken', () => {
  it('returns undefined for undefined input', () => {
    expect(sanitizeToken(undefined)).toBeUndefined();
  });

  it('returns undefined for non-string input', () => {
    expect(sanitizeToken(123 as unknown as string)).toBeUndefined();
    expect(sanitizeToken(NaN as unknown as string)).toBeUndefined();
    expect(sanitizeToken({} as unknown as string)).toBeUndefined();
    expect(sanitizeToken([] as unknown as string)).toBeUndefined();
    expect(sanitizeToken(null as unknown as string)).toBeUndefined();
    expect(sanitizeToken(true as unknown as string)).toBeUndefined();
    expect(sanitizeToken(false as unknown as string)).toBeUndefined();
    expect(sanitizeToken(Symbol('token') as unknown as string)).toBeUndefined();
    expect(sanitizeToken(10n as unknown as string)).toBeUndefined();
    expect(sanitizeToken((() => {}) as unknown as string)).toBeUndefined();
  });

  it('accepts representative valid token string and preserves documented result', () => {
    // Verifies the `typeof token === 'string'` control-flow branch accepts valid inputs
    // and preserves the exact token string value without alteration.
    const representativeToken = 'sb_live_secret_key_12345';
    expect(sanitizeToken(representativeToken)).toBe(representativeToken);

    // Also assert various representative valid token formats (hex, uuid, jwt, single char)
    const validTokens = [
      'sb_sec_0123456789abcdef',
      'bearer-token-uuid-1234-5678',
      'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.signature',
      'valid_token_without_whitespace',
      'a',
    ];
    for (const token of validTokens) {
      expect(sanitizeToken(token)).toBe(token);
    }
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

describe('TokenHolder', () => {
  it('initializes with undefined when no token provided', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('accepts representative valid token and preserves state', () => {
    const token = 'sb_live_secret_key_12345';
    const holder = new TokenHolder(token);
    expect(holder.get()).toBe(token);
    expect(holder.hasToken()).toBe(true);
  });

  it('rotates token value and updates hasToken state', () => {
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
