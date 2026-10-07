import { describe, expect, it, vi } from 'vitest';

import {
  createStellarBillClient,
  StellarBillConfigError,
  type StellarBillClientOptions,
} from '../src/index.js';

/**
 * Focused boundary coverage for the "required" guard in `validateBaseUrl`
 * (`sdks/ts/src/client.ts:83`):
 *
 *   function validateBaseUrl(raw: unknown): string {
 *     if (raw === undefined || raw === null) {          // ← branch under test
 *       throw new StellarBillConfigError('baseUrl is required');
 *     }
 *     if (typeof raw !== 'string' || raw.trim().length === 0) {
 *       throw new StellarBillConfigError('baseUrl must be a non-empty string');
 *     }
 *     …
 *   }
 *
 * The guard fires on exactly two values — `undefined` and `null` — and it uses
 * strict identity, not truthiness. That distinction is observable: an empty
 * string, `0`, `false`, or `NaN` are falsy but must NOT be reported as
 * "required"; they fall through to the adjacent "non-empty string" guard.
 *
 * The tests below pin:
 *   1. the failure contract for `undefined` / `null` (class, name, message),
 *   2. that omitted / explicit-`undefined` properties are indistinguishable,
 *   3. the strict-identity boundary against every other falsy value,
 *   4. determinism and "no request is attempted",
 *   5. the neighboring success path so the guard cannot over-reject.
 *
 * The branch is reached through the public `createStellarBillClient()` surface,
 * so every assertion is observable to callers.
 */

type FetchCall = { url: string };

/** Records requested URLs and answers every call with a fresh 2xx JSON body. */
function mockFetch(): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetch: typeof globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    calls.push({ url: input instanceof Request ? input.url : String(input) });
    // A fresh Response per call: an HTTP body can only be consumed once.
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch, calls };
}

/** Runs `fn` and returns the thrown value (or `undefined` if it did not throw). */
function captureError(fn: () => unknown): unknown {
  try {
    fn();
  } catch (err) {
    return err;
  }
  return undefined;
}

const BASE = 'https://api.example.com';

// ---------------------------------------------------------------------------
// 1. Failure contract — the branch fires for `undefined` and `null`.
// ---------------------------------------------------------------------------

describe('validateBaseUrl required branch - fires for undefined/null (client.ts:83)', () => {
  it('throws StellarBillConfigError "baseUrl is required" for undefined', () => {
    const caught = captureError(() =>
      createStellarBillClient({ baseUrl: undefined as unknown as string, fetch: mockFetch().fetch }),
    );
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  it('throws StellarBillConfigError "baseUrl is required" for null', () => {
    const caught = captureError(() =>
      createStellarBillClient({ baseUrl: null as unknown as string, fetch: mockFetch().fetch }),
    );
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  it('treats an omitted baseUrl property as undefined (same branch)', () => {
    // `{}` has no own `baseUrl`, so `options.baseUrl` is `undefined`.
    const caught = captureError(() =>
      createStellarBillClient({} as unknown as StellarBillClientOptions),
    );
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  it('treats an explicit undefined property value as undefined (same branch)', () => {
    const caught = captureError(() =>
      createStellarBillClient({
        baseUrl: undefined,
        fetch: mockFetch().fetch,
      } as unknown as StellarBillClientOptions),
    );
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  it('sets the exact error class / name / message contract', () => {
    const caught = captureError(() =>
      createStellarBillClient({ baseUrl: null as unknown as string, fetch: mockFetch().fetch }),
    );
    const err = caught as StellarBillConfigError;
    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe('StellarBillConfigError');
    expect(err.message).toBe('baseUrl is required');
  });

  it('is deterministic: repeated calls throw the same class and message', () => {
    for (const raw of [undefined, null]) {
      const first = captureError(() =>
        createStellarBillClient({ baseUrl: raw as unknown as string, fetch: mockFetch().fetch }),
      );
      const second = captureError(() =>
        createStellarBillClient({ baseUrl: raw as unknown as string, fetch: mockFetch().fetch }),
      );
      expect(first).toBeInstanceOf(StellarBillConfigError);
      expect(second).toBeInstanceOf(StellarBillConfigError);
      expect((first as StellarBillConfigError).message).toBe((second as StellarBillConfigError).message);
      expect((first as StellarBillConfigError).message).toBe('baseUrl is required');
    }
  });

  it('never invokes fetch and never returns a client when the branch throws', () => {
    const { fetch, calls } = mockFetch();
    let client: ReturnType<typeof createStellarBillClient> | undefined;
    const caught = captureError(() => {
      client = createStellarBillClient({ baseUrl: undefined as unknown as string, fetch });
    });
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect(client).toBeUndefined();
    // validateBaseUrl runs before any request, so the transport stays untouched.
    expect(calls).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// 2. Strict-identity boundary — falsy values that are NOT undefined/null must
//    reach the adjacent guard, proving the branch is `=== undefined || === null`
//    and not a truthiness check (`!raw`).
// ---------------------------------------------------------------------------

describe('validateBaseUrl required branch - strict identity, not truthiness', () => {
  it.each<[string, unknown]>([
    ['empty string', ''],
    ['whitespace-only string', '   '],
    ['number 0', 0],
    ['NaN', Number.NaN],
    ['boolean false', false],
    ['empty array', []],
    ['plain object', {}],
    ['function', () => 'https://api.example.com'],
    ['symbol', Symbol('baseUrl')],
    ['boxed String', new String('https://api.example.com')],
  ])('reports the non-empty-string error (not "required") for %s', (_label, value) => {
    const caught = captureError(() =>
      createStellarBillClient({ baseUrl: value as unknown as string, fetch: mockFetch().fetch }),
    );
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    const err = caught as StellarBillConfigError;
    expect(err.message).toBe('baseUrl must be a non-empty string');
    // A truthiness check would have misclassified these as "required".
    expect(err.message).not.toBe('baseUrl is required');
  });

  it('never emits the "required" message for any falsy non-null value', () => {
    const falsyButPresent: unknown[] = ['', '   ', 0, -0, Number.NaN, false];
    for (const value of falsyButPresent) {
      const caught = captureError(() =>
        createStellarBillClient({ baseUrl: value as unknown as string, fetch: mockFetch().fetch }),
      );
      expect((caught as StellarBillConfigError).message).not.toBe('baseUrl is required');
    }
  });
});

// ---------------------------------------------------------------------------
// 3. Success path — a valid baseUrl does not hit the guard, a client is built,
//    and the request composes against the normalized base.
// ---------------------------------------------------------------------------

describe('validateBaseUrl required branch - success path is unaffected', () => {
  it('constructs a client for a valid URL without throwing', () => {
    const { fetch } = mockFetch();
    let client: ReturnType<typeof createStellarBillClient> | undefined;
    expect(() => {
      client = createStellarBillClient({ baseUrl: BASE, fetch });
    }).not.toThrow();
    expect(client).toBeDefined();
  });

  it('composes the health request against the validated base URL', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({ baseUrl: BASE, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url).toBe(`${BASE}/api/health`);
  });

  it('still accepts and normalizes a base URL with trailing slashes', async () => {
    const { fetch, calls } = mockFetch();
    const sdk = createStellarBillClient({ baseUrl: `${BASE}///`, fetch });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(calls[0]!.url).toBe(`${BASE}/api/health`);
  });

  it('does not throw the "required" error for a valid URL', () => {
    const { fetch } = mockFetch();
    expect(() => createStellarBillClient({ baseUrl: BASE, fetch })).not.toThrow(
      StellarBillConfigError,
    );
  });
});
