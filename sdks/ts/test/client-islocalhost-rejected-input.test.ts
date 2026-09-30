import { describe, expect, it } from 'vitest';

import { isLocalhost } from '../src/index.js';

/**
 * Direct unit coverage for `isLocalhost` (issue #876, isLocalhost at
 * sdks/ts/src/client.ts).
 *
 * The existing suite (`client-localhost-guard.test.ts`,
 * `client-localhost-accepted-input.test.ts`) only exercises the function
 * indirectly, through the insecure-baseUrl warning emitted by
 * `createStellarBillClient`. These tests call it directly and focus on the
 * rejected/malformed-input path: everything that fails `new URL(...)` must
 * return `false` and must never throw.
 *
 * The guard is:
 *
 *   try {
 *     const u = new URL(baseUrl);
 *     return u.hostname === 'localhost' || u.hostname === '127.0.0.1';
 *   } catch {
 *     return false;
 *   }
 */

// Inputs that `new URL(...)` rejects (or that do not parse as an absolute URL
// with a loopback hostname). None of these may throw; all must return false.
const MALFORMED_INPUTS = [
  '',
  'not a url',
  'localhost',
  '127.0.0.1',
  'http://',
  '://x',
  'http://[',
  ' ht tp://localhost ',
] as const;

const NON_LOOPBACK_HOSTS = [
  'http://example.com',
  'http://localhost.evil.com',
  'http://127.0.0.2',
  'https://api.example.com',
  'http://[::1]',
] as const;

const LOOPBACK_HOSTS = [
  'http://localhost',
  'http://localhost:3000',
  'http://127.0.0.1',
  'http://127.0.0.1:8080',
  'http://LOCALHOST:3000',
  'http://user:pass@localhost:9000',
] as const;

describe('isLocalhost - rejected / malformed input', () => {
  it.each(MALFORMED_INPUTS)('returns false for %j', (input) => {
    expect(isLocalhost(input)).toBe(false);
  });

  it.each(MALFORMED_INPUTS)('never throws for %j', (input) => {
    expect(() => isLocalhost(input)).not.toThrow();
  });

  it('returns false for every malformed input and never throws', () => {
    for (const input of MALFORMED_INPUTS) {
      expect(() => isLocalhost(input)).not.toThrow();
      expect(isLocalhost(input)).toBe(false);
    }
  });
});

describe('isLocalhost - non-loopback hosts', () => {
  it.each(NON_LOOPBACK_HOSTS)('returns false for %j', (input) => {
    expect(isLocalhost(input)).toBe(false);
  });

  it('treats bracketed IPv6 loopback as non-localhost (current behaviour)', () => {
    // `URL#hostname` retains the square brackets for IPv6 hosts, so
    // `new URL('http://[::1]').hostname === '[::1]'`, which never equals the
    // bare literals checked by the guard. Pinned as current behaviour: this
    // test deliberately does not assert a desired future fix.
    expect(new URL('http://[::1]').hostname).toBe('[::1]');
    expect(isLocalhost('http://[::1]')).toBe(false);
  });
});

describe('isLocalhost - loopback hosts', () => {
  it.each(LOOPBACK_HOSTS)('returns true for %j', (input) => {
    expect(isLocalhost(input)).toBe(true);
  });

  it('lowercases the hostname before comparing (URL canonicalisation)', () => {
    expect(isLocalhost('http://LOCALHOST:3000')).toBe(true);
    expect(isLocalhost('http://LocalHost')).toBe(true);
  });
});

describe('isLocalhost - determinism', () => {
  const REPEATED = [...MALFORMED_INPUTS, ...NON_LOOPBACK_HOSTS, ...LOOPBACK_HOSTS] as const;

  it.each(REPEATED)('returns the same result on repeated calls for %j', (input) => {
    const first = isLocalhost(input);
    const second = isLocalhost(input);
    const third = isLocalhost(input);
    expect(second).toBe(first);
    expect(third).toBe(first);
  });
});
