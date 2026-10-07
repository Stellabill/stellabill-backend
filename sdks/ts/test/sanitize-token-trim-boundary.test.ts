import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, sanitizeToken } from '../src/index.js';

/**
 * Focused coverage for the "empty after trimming" guard inside `sanitizeToken`
 * (`sdks/ts/src/auth.ts:43`):
 *
 *   const trimmed = token.trim();
 *   if (trimmed.length === 0) return undefined;   // ← branch under test
 *   if (/\s/.test(trimmed)) return undefined;
 *   return trimmed;
 *
 * The guard is the boundary between two observable outcomes:
 *
 *   • REJECT — the input is a string whose only content is whitespace, so it
 *     collapses to `''` after trimming. The guard fires and the result is
 *     `undefined` (never `''` and never a throw).
 *
 *   • ACCEPT — the input has at least one non-whitespace character after
 *     trimming (`trimmed.length > 0`), so the guard does not fire. When the
 *     remaining text has no internal whitespace, the *trimmed* token is
 *     returned, preserving the documented normalization (`" abc "` → `"abc"`).
 *
 * A third, adjacent outcome (`/\s/.test(trimmed)` firing on internal
 * whitespace) is pinned alongside the guard so the two branches cannot be
 * confused: `"a b"` is rejected by the *second* guard, not this one.
 *
 * Every input is exercised both directly via `sanitizeToken()` and, in the
 * final block, through the public SDK surface so the branch's effect on the
 * request (`Authorization: Bearer <token>`) is observable and deterministic.
 */

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// 1. REJECT side — `trimmed.length === 0` fires for whitespace-only inputs.
//    These are non-empty strings that become empty *after* trimming, so they
//    must funnel to `undefined` rather than a silently-empty token.
// ---------------------------------------------------------------------------

describe('sanitizeToken trim guard - rejected side (trimmed.length === 0)', () => {
  it('returns undefined for the empty string', () => {
    expect(sanitizeToken('')).toBeUndefined();
  });

  it.each<[string, string]>([
    ['single space', ' '],
    ['multiple spaces', '   '],
    ['tab', '\t'],
    ['newline', '\n'],
    ['carriage return', '\r'],
    ['CRLF', '\r\n'],
    ['mixed whitespace', ' \t\n\r '],
    ['non-breaking space', '\u00A0'],
    ['ideographic space', '\u3000'],
    ['byte-order mark / ZWNBSP', '\uFEFF'],
    ['mixed unicode whitespace', '\u00A0\u3000\uFEFF'],
  ])('returns undefined for a whitespace-only token (%s)', (_label, token) => {
    // Sanity-check the premise: the input is non-empty but trims to empty.
    expect(token.length).toBeGreaterThan(0);
    expect(token.trim()).toBe('');
    expect(sanitizeToken(token)).toBeUndefined();
  });

  it('never returns the empty string for whitespace-only input', () => {
    // The guard returns `undefined`, not `''`; an empty string would be a
    // silently accepted-but-useless token. Pin the exact sentinel.
    for (const token of ['', ' ', '\t', '\u00A0', '\u3000', '\uFEFF']) {
      expect(sanitizeToken(token)).not.toBe('');
      expect(sanitizeToken(token)).toBeUndefined();
    }
  });

  it('is deterministic: repeated calls agree for the rejected side', () => {
    for (const token of ['', '   ', '\t\n', '\uFEFF']) {
      expect(sanitizeToken(token)).toBe(sanitizeToken(token));
      expect(sanitizeToken(token)).toBeUndefined();
    }
  });
});

// ---------------------------------------------------------------------------
// 2. ACCEPT side — `trimmed.length > 0` does NOT fire, so the trimmed token is
//    preserved. Surrounding whitespace is stripped; the body is returned as-is.
// ---------------------------------------------------------------------------

describe('sanitizeToken trim guard - accepted side (trimmed.length > 0)', () => {
  it('returns a bare token unchanged', () => {
    expect(sanitizeToken('abc')).toBe('abc');
    expect(sanitizeToken('a-b_C.123')).toBe('a-b_C.123');
  });

  it('returns the trimmed token when surrounding spaces are stripped', () => {
    expect(sanitizeToken('  abc  ')).toBe('abc');
    expect(sanitizeToken(' abc ')).toBe('abc');
  });

  it('returns the trimmed token for tab / newline padding', () => {
    expect(sanitizeToken('\tabc\n')).toBe('abc');
    expect(sanitizeToken('\r\nabc\r\n')).toBe('abc');
    expect(sanitizeToken(' \t abc \n ')).toBe('abc');
  });

  it('returns the trimmed token for unicode whitespace padding', () => {
    // `.trim()` strips NBSP / ideographic space / ZWNBSP at the edges, so the
    // remaining body has length > 0 and no internal whitespace → accepted.
    expect(sanitizeToken('\u00A0abc\u3000')).toBe('abc');
    expect(sanitizeToken('\uFEFFabc')).toBe('abc');
  });

  it('accepts the minimal length-one token on either side of the boundary', () => {
    expect(sanitizeToken('x')).toBe('x');
    expect(sanitizeToken(' x ')).toBe('x');
    expect(sanitizeToken('\u00A0x\u00A0')).toBe('x');
  });

  it('always equals token.trim() when the trimmed body has no internal whitespace', () => {
    const accepted = ['abc', '  abc  ', '\tabc\n', ' x ', '\u00A0abc\u3000', 'a-b_C.123'];
    for (const token of accepted) {
      const trimmed = token.trim();
      expect(trimmed.length).toBeGreaterThan(0);
      expect(/\s/.test(trimmed)).toBe(false);
      expect(sanitizeToken(token)).toBe(trimmed);
    }
  });
});

// ---------------------------------------------------------------------------
// 3. Separation from the adjacent internal-whitespace guard. These inputs pass
//    the length guard (`trimmed.length > 0`) and are rejected by `/\s/` instead.
// ---------------------------------------------------------------------------

describe('sanitizeToken trim guard - separation from the internal-whitespace guard', () => {
  it('rejects internal whitespace via the second guard, not the length guard', () => {
    for (const token of ['a b', '  a b  ', 'a\tb', 'a\nb']) {
      const trimmed = token.trim();
      // The length guard does NOT fire here...
      expect(trimmed.length).toBeGreaterThan(0);
      // ...so the result is undefined because of the internal-whitespace guard.
      expect(sanitizeToken(token)).toBeUndefined();
    }
  });

  it('distinguishes whitespace-only input (length guard) from padded valid input (accepted)', () => {
    expect(sanitizeToken('   ')).toBeUndefined(); // length guard fires
    expect(sanitizeToken(' a ')).toBe('a'); // length guard does not fire
  });
});

// ---------------------------------------------------------------------------
// 4. Observable token / request behavior — the guard's decision must map onto
//    what the client reports via getToken() and what it puts on the wire.
// ---------------------------------------------------------------------------

type FetchCall = { headers: Record<string, string> };

function captureHeaders(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    calls.push({ headers });
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch, calls };
}

const BASE = 'https://api.example.com';

describe('sanitizeToken trim guard - observable request behavior', () => {
  it('sends the trimmed token for a whitespace-padded constructor token', async () => {
    const { fetch, calls } = captureHeaders();
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '  wire-token  ', fetch });

    expect(sdk.getToken()).toBe('wire-token');
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBe('Bearer wire-token');
  });

  it('omits Authorization when the constructor token is whitespace-only', async () => {
    const { fetch, calls } = captureHeaders();
    const sdk = createStellarBillClient({ baseUrl: BASE, token: '   ', fetch });

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('sends the trimmed token after setToken() with surrounding whitespace', async () => {
    const { fetch, calls } = captureHeaders();
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'first', fetch });
    sdk.setToken('  rotated-token  ');

    expect(sdk.getToken()).toBe('rotated-token');
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBe('Bearer rotated-token');
  });

  it('clears the token and the header when setToken() is whitespace-only', async () => {
    const { fetch, calls } = captureHeaders();
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'live', fetch });
    sdk.setToken('\t\n ');

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('omits Authorization for a token rejected by the internal-whitespace guard', async () => {
    const { fetch, calls } = captureHeaders();
    const sdk = createStellarBillClient({ baseUrl: BASE, token: 'not a token', fetch });

    expect(sdk.getToken()).toBeUndefined();
    await sdk.getHealth();
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });
});
