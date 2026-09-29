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

