import { describe, expect, it } from 'vitest';

import { createStellarBillClient, sanitizeToken, TokenHolder } from '../src/index.js';

// ---------------------------------------------------------------------------
// Helpers shared across tests
// ---------------------------------------------------------------------------

/** Minimal mock fetch used only for client-level token propagation tests. */
function stubFetch(
  body: unknown = {},
  status = 200,
): { fetch: typeof globalThis.fetch; capturedHeaders: () => Record<string, string> } {
  let captured: Record<string, string> = {};
  const fetch: typeof globalThis.fetch = async (input) => {
    captured = {};
    if (input instanceof Request) {
      input.headers.forEach((v, k) => {
        captured[k.toLowerCase()] = v;
      });
    }
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    });
  };
  return { fetch, capturedHeaders: () => captured };
}

// ---------------------------------------------------------------------------
// sanitizeToken — foundational guard tests (pre-existing)
// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
// sanitizeToken — boundary conditions for `return trimmed` (auth.ts:39)
//
// This branch is reached only when ALL of the following hold:
//   1. token !== undefined
//   2. typeof token === 'string'
//   3. token.trim().length > 0          (not all-whitespace)
//   4. /\s/.test(trimmed) === false     (no internal whitespace after trimming)
//
// The tests below pin the exact edges of this branch so that any silent
// drift in the sanitization contract is caught immediately.
// ---------------------------------------------------------------------------

describe('sanitizeToken — return trimmed boundary (auth.ts:39)', () => {
  // ── 1. Minimum-length result ──────────────────────────────────────────────
  // After trimming, the shortest possible valid token is a single character.

  it('returns the single character when exactly one non-whitespace char is surrounded by spaces', () => {
    // Boundary: ' a ' trims to 'a' (length 1, no internal whitespace → return trimmed)
    expect(sanitizeToken(' a ')).toBe('a');
  });

  it('returns a single non-whitespace character without any padding', () => {
    // Confirms the no-op trim path also reaches return trimmed.
    expect(sanitizeToken('x')).toBe('x');
  });

  // ── 2. Each individual whitespace type as padding ─────────────────────────
  // JS String.prototype.trim() strips the full set of Unicode whitespace
  // characters (space, tab, LF, CR, FF, VT, NBSP, …). The regex /\s/ that
  // guards against *internal* whitespace matches the same set. We verify
  // that each whitespace type when used as *only* padding still routes to
  // `return trimmed` and yields the inner token unchanged.

  it('strips leading/trailing horizontal tab (\\t) and returns the inner token', () => {
    expect(sanitizeToken('\tmy-token\t')).toBe('my-token');
  });

  it('strips leading/trailing newline (\\n) and returns the inner token', () => {
    expect(sanitizeToken('\nmy-token\n')).toBe('my-token');
  });

  it('strips leading/trailing carriage return (\\r) and returns the inner token', () => {
    expect(sanitizeToken('\rmy-token\r')).toBe('my-token');
  });

  it('strips leading/trailing form feed (\\f) and returns the inner token', () => {
    expect(sanitizeToken('\fmy-token\f')).toBe('my-token');
  });

  it('strips leading/trailing vertical tab (\\v) and returns the inner token', () => {
    expect(sanitizeToken('\vmy-token\v')).toBe('my-token');
  });

  // ── 3. Mixed whitespace padding ───────────────────────────────────────────
  // Real-world tokens copied from JSON or .env files may carry mixed
  // leading/trailing whitespace of different types.

  it('strips a mixed leading/trailing whitespace sequence (space + tab + newline)', () => {
    expect(sanitizeToken(' \t\nmy-token\n\t ')).toBe('my-token');
  });

  it('strips CR+LF line endings as padding (Windows-style)', () => {
    expect(sanitizeToken('\r\nmy-token\r\n')).toBe('my-token');
  });

  // ── 4. The `return trimmed` value is identical to `token.trim()` ──────────
  // Confirms no further mutation occurs: the returned value is exactly the
  // trimmed string, not an additional transformation.

  it('returns the exact trimmed value — no additional mutation applied', () => {
    const inner = 'eyJhbGciOiJIUzI1NiJ9.payload.sig';
    expect(sanitizeToken(`  ${inner}  `)).toBe(inner);
  });

  // ── 5. Boundary with the adjacent `return undefined` branch ──────────────
  // The guard `/\s/.test(trimmed)` sits immediately before `return trimmed`.
  // A token with *internal* whitespace must NOT reach the return-trimmed
  // branch; it should fall to the `return undefined` on line 38.

  it('does NOT return trimmed when the trimmed string still has internal spaces', () => {
    // 'a b' trims to 'a b'; /\s/.test('a b') === true → return undefined, not 'a b'
    expect(sanitizeToken('  a b  ')).toBeUndefined();
  });

  it('does NOT return trimmed when the trimmed string has an internal tab', () => {
    expect(sanitizeToken('part1\tpart2')).toBeUndefined();
  });

  it('does NOT return trimmed when the trimmed string has an internal newline', () => {
    expect(sanitizeToken('part1\npart2')).toBeUndefined();
  });

  // ── 6. Unicode non-breaking space (U+00A0) — behavior is position-dependent ─
  // V8 / ECMAScript: String.prototype.trim() strips U+00A0 when it appears in
  // a leading or trailing position (trim() uses the full Unicode whitespace
  // definition). /\s/ also matches U+00A0.
  //
  // Consequence:
  //   • U+00A0 at start/end only  → stripped by trim() → never reaches /\s/ guard
  //                                 → `return trimmed` with clean token (same as space)
  //   • U+00A0 embedded           → NOT stripped by trim() → caught by /\s/ guard
  //                                 → `return undefined`
  //
  // Both behaviors are consistent with the existing whitespace-handling rules.

  it('strips leading/trailing U+00A0 and returns the inner token', () => {
    // U+00A0 at both ends is stripped by trim() in V8 (Unicode whitespace).
    expect(sanitizeToken('\u00A0mytoken\u00A0')).toBe('mytoken');
  });

  it('rejects a token with embedded U+00A0 — caught by internal-whitespace guard (/\\s/ matches U+00A0)', () => {
    // U+00A0 embedded survives trim(), then /\s/ matches it → return undefined.
    expect(sanitizeToken('tok\u00A0en')).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// TokenHolder — downstream propagation of the trimmed value
//
// sanitizeToken is called at two points where the trimmed result matters
// downstream:
//   • createStellarBillClient({ token }) → stored in TokenHolder
//   • client.setToken(token)             → replaces value in TokenHolder
//
// These tests confirm that `return trimmed` (not the original padded string)
// is what flows through the rest of the SDK and ultimately reaches the
// Authorization header.
// ---------------------------------------------------------------------------

describe('TokenHolder — trimmed value stored and retrieved', () => {
  it('stores the trimmed string when constructed with a padded token', () => {
    const holder = new TokenHolder('  bearer-123  ');
    // TokenHolder stores whatever sanitizeToken returns, but TokenHolder itself
    // does not call sanitizeToken — the client does. We test TokenHolder
    // directly to confirm its contract: whatever is passed to the constructor
    // is stored as-is.
    expect(holder.get()).toBe('  bearer-123  ');
    expect(holder.hasToken()).toBe(true);
  });

  it('hasToken returns false for an empty string', () => {
    const holder = new TokenHolder('');
    expect(holder.hasToken()).toBe(false);
    expect(holder.get()).toBe('');
  });

  it('hasToken returns false when no initial token is supplied', () => {
    const holder = new TokenHolder();
    expect(holder.hasToken()).toBe(false);
    expect(holder.get()).toBeUndefined();
  });

  it('set() replaces the stored value', () => {
    const holder = new TokenHolder('old');
    holder.set('new');
    expect(holder.get()).toBe('new');
    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Client-level propagation: setToken() → Authorization header
//
// These tests verify that the trimmed value returned by sanitizeToken at
// auth.ts:39 is what actually reaches the wire. This closes the observable
// contract: if the branch silently changed (e.g. started returning the raw
// value), these assertions would fail.
// ---------------------------------------------------------------------------

describe('sanitizeToken return trimmed — downstream Authorization header propagation', () => {
  const BASE = 'https://api.example.com';
  const HEALTH_BODY = { status: 'ok', service: 'stellarbill-backend' };

  it('constructor: token with spaces becomes trimmed in Bearer header', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '  my-token  ', fetch });
    await sdk.getHealth();
    // The Authorization header must carry the trimmed value, not the padded one.
    expect(capturedHeaders()['authorization']).toBe('Bearer my-token');
  });

  it('constructor: token with tab padding becomes trimmed in Bearer header', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '\tmy-token\t', fetch });
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer my-token');
  });

  it('constructor: token with newline padding becomes trimmed in Bearer header', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '\nmy-token\n', fetch });
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer my-token');
  });

  it('setToken: padded token is trimmed before being stored', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    // Initially no token.
    expect(sdk.getToken()).toBeUndefined();

    // Set a token with surrounding whitespace.
    sdk.setToken('  rotated-tok  ');

    // getToken must expose the trimmed value.
    expect(sdk.getToken()).toBe('rotated-tok');

    // The Authorization header on the next request must use the trimmed value.
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer rotated-tok');
  });

  it('setToken: token with tab+newline padding is trimmed before being stored', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('\t\nrotated-tok\n\t');
    expect(sdk.getToken()).toBe('rotated-tok');
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer rotated-tok');
  });

  it('setToken: token with internal whitespace is rejected — no Authorization header emitted', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    // This token has internal whitespace; sanitizeToken returns undefined.
    sdk.setToken('bad token');
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBeUndefined();
  });

  it('setToken: token that is only whitespace is rejected — no Authorization header emitted', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });
    sdk.setToken('   ');
    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBeUndefined();
  });

  it('single-character token with padding propagates trimmed single char to header', async () => {
    const { fetch, capturedHeaders } = stubFetch(HEALTH_BODY);
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '  a  ', fetch });
    await sdk.getHealth();
    expect(capturedHeaders()['authorization']).toBe('Bearer a');
  });
});
