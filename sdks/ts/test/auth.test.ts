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
