import { describe, expect, it, vi } from 'vitest';

import { TokenHolder, createStellarBillClient, sanitizeToken } from '../src/index.js';

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
