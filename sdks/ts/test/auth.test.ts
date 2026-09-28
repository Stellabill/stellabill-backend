import { describe, expect, it, vi } from 'vitest';

import { TokenHolder, createStellarBillClient, sanitizeToken } from '../src/index.js';

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

// ── Boundary conditions on the empty/whitespace branches (src/auth.ts:34-37) ──

describe('sanitizeToken - boundary conditions', () => {
  it.each([
    ['a single space', ' '],
    ['a tab', '\t'],
    ['a newline', '\n'],
    ['a carriage return + newline', '\r\n'],
    ['a non-breaking space', '\u00a0'],
    ['a mixed whitespace run', ' \t\n\r\u00a0 '],
  ])('rejects %s as empty after trimming', (_label, value) => {
    expect(sanitizeToken(value)).toBeUndefined();
  });

  it.each([
    ['a tab', 'a\tb'],
    ['a newline', 'a\nb'],
    ['a non-breaking space', 'a\u00a0b'],
    ['a vertical tab', 'a\u000bb'],
  ])('rejects a token whose body still contains %s', (_label, value) => {
    expect(sanitizeToken(value)).toBeUndefined();
  });

  it('distinguishes nullish input from an empty string', () => {
    // Both collapse to undefined, but through different branches: `undefined`
    // hits the early return, `''` is trimmed to length 0.
    expect(sanitizeToken(undefined)).toBeUndefined();
    expect(sanitizeToken('')).toBeUndefined();
  });

  it('preserves every non-whitespace character of a valid token', () => {
    const token = 'eyJhbGciOi.eyJzdWIiOi-1_signature~+/=';

    expect(sanitizeToken(token)).toBe(token);
  });

  it('keeps zero-width space characters (not part of JS trim/\\s)', () => {
    // U+200B is neither trimmed nor matched by \s, so it survives verbatim.
    expect(sanitizeToken('a\u200bb')).toBe('a\u200bb');
  });

  it('is idempotent', () => {
    const once = sanitizeToken('  token-value  ');
    expect(sanitizeToken(once)).toBe('token-value');
  });

  it('handles a very long token without truncation', () => {
    const token = 'x'.repeat(10_000);
    expect(sanitizeToken(token)).toBe(token);
  });

  it('does not strip the scheme-like prefix from a token', () => {
    expect(sanitizeToken('Bearer test-value')).toBeUndefined(); // embedded space, so rejected
    expect(sanitizeToken('Bearer')).toBe('Bearer'); // plain ASCII is passed through
  });
});

// ── TokenHolder ─────────────────────────────────────────────────────────────
//
// `TokenHolder` is deliberately raw: it stores exactly what it is given and
// `sanitizeToken` is applied by the client before the holder sees a value.

describe('TokenHolder', () => {
  it('starts empty when constructed without an initial token', () => {
    const holder = new TokenHolder();

    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('stores the initial token', () => {
    const holder = new TokenHolder('initial');

    expect(holder.get()).toBe('initial');
    expect(holder.hasToken()).toBe(true);
  });

  it('rotates the token on set()', () => {
    const holder = new TokenHolder('old');

    holder.set('new');
    expect(holder.get()).toBe('new');

    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('reports an empty string as "no token" while still returning it from get()', () => {
    const holder = new TokenHolder('');

    expect(holder.get()).toBe('');
    expect(holder.hasToken()).toBe(false);
  });

  it('does not sanitize values handed to set() (raw storage)', () => {
    const holder = new TokenHolder();

    holder.set('   ');
    expect(holder.get()).toBe('   ');
    // Non-empty strings — including whitespace — count as "present".
    expect(holder.hasToken()).toBe(true);
  });
});

// ── sanitizeToken <-> createStellarBillClient wiring ────────────────────────
//
// The client is the only place that sanitizes: a raw token must never reach an
// `Authorization` header, and rotation must apply the same rules.

function captureFetch() {
  const headers: Record<string, string> = {};
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request =
      input instanceof Request
        ? input
        : new Request(typeof input === 'string' ? input : input.toString(), init);
    request.headers.forEach((value, key) => {
      headers[key.toLowerCase()] = value;
    });
    return new Response(JSON.stringify({ status: 'ok' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, headers };
}

describe('token sanitization at the client boundary', () => {
  it('trims the configured token before storing and sending it', async () => {
    const { fetch, headers } = captureFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: '  token-value  ',
      fetch,
    });

    expect(sdk.getToken()).toBe('token-value');
    await sdk.getHealth();

    expect(headers['authorization']).toBe('Bearer token-value');
  });

  it('drops a whitespace-only token entirely', async () => {
    const { fetch, headers } = captureFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: ' \t\n ',
      fetch,
    });

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();

    expect(headers['authorization']).toBeUndefined();
  });

  it('applies the same sanitization on setToken()', async () => {
    const { fetch, headers } = captureFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    sdk.setToken('  rotated  ');
    expect(sdk.getToken()).toBe('rotated');

    sdk.setToken('   ');
    expect(sdk.getToken()).toBeUndefined();

    await sdk.getHealth();
    expect(headers['authorization']).toBeUndefined();
  });

  it('never sends a token containing internal whitespace', async () => {
    const { fetch, headers } = captureFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'bad token',
      fetch,
    });

    await sdk.getHealth();

    expect(headers['authorization']).toBeUndefined();
  });
});
