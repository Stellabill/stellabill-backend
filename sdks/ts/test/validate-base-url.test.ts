/**
 * Regression tests for the `validateBaseUrl` invalid-URL rejection branch.
 *
 * Issue #870 — `sdks/ts/src/client.ts:93`
 *
 * The branch under test:
 *   ```ts
 *   throw new StellarBillConfigError(`baseUrl "${raw}" is not a valid URL`);
 *   ```
 *
 * This branch is reached when a non-empty string is supplied as `baseUrl` but
 * `new URL(raw)` throws (i.e. the string is syntactically not a URL).
 * The tests in this file exercise:
 *
 *   1. The rejected-input branch — multiple representative invalid strings.
 *   2. The exact `StellarBillConfigError` contract (class, name, message).
 *   3. No client state is produced when the input is invalid.
 *   4. The neighboring valid success path.
 *   5. Boundary inputs across all three rejection branches inside
 *      `validateBaseUrl` so a future refactor cannot silently merge them.
 *
 * No real HTTP requests are made. `globalThis.fetch` is replaced with a
 * minimal stub only where a client object must be constructed.
 */

import { describe, expect, it } from 'vitest';

import { createStellarBillClient } from '../src/index.js';
import { StellarBillConfigError } from '../src/index.js';

// ---------------------------------------------------------------------------
// Minimal no-op fetch stub used only when a fully-constructed client is needed
// for a valid-URL success path check. It should never be called in invalid-URL
// tests; we verify that through `fetchCallCount`.
// ---------------------------------------------------------------------------
function makeStubFetch(): { fetch: typeof globalThis.fetch; callCount: () => number } {
  let n = 0;
  const fetch: typeof globalThis.fetch = async (_input, _init) => {
    n++;
    return new Response(JSON.stringify({ status: 'ok', service: 'stellarbill-backend' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  };
  return { fetch, callCount: () => n };
}

// ---------------------------------------------------------------------------
// 1. Rejected-input branch — Issue #870 target
//    Each input must be a non-empty string that passes the earlier guards
//    (not undefined/null, not non-string, not empty/whitespace) but fails
//    `new URL(raw)`, causing the branch at client.ts:93 to throw.
// ---------------------------------------------------------------------------
describe('validateBaseUrl — invalid URL rejection branch (Issue #870)', () => {
  // Helper: assert the branch fires for a given raw string.
  function expectInvalidUrl(raw: string): void {
    expect(() => createStellarBillClient({ baseUrl: raw, fetch: makeStubFetch().fetch })).toThrow(
      StellarBillConfigError,
    );
  }

  // Helper: assert both class and exact message for the representative case.
  function expectInvalidUrlMessage(raw: string): void {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: raw, fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    const e = caught as StellarBillConfigError;
    // The contract: name must be 'StellarBillConfigError'.
    expect(e.name).toBe('StellarBillConfigError');
    // The contract: message must embed the rejected raw value.
    expect(e.message).toContain(raw);
    // The contract: message follows the documented template.
    expect(e.message).toBe(`baseUrl "${raw}" is not a valid URL`);
    // instanceof must work (Object.setPrototypeOf is applied in the constructor).
    expect(caught).toBeInstanceOf(Error);
  }

  it('rejects a bare hostname with no scheme — exercises the target branch', () => {
    // "example.com" has no scheme; `new URL("example.com")` throws in Node.
    expectInvalidUrlMessage('example.com');
  });

  it('rejects a protocol-relative URL (//example.com)', () => {
    expectInvalidUrl('//example.com');
  });

  it('rejects a string with a scheme-like prefix but no authority (http:/)', () => {
    expectInvalidUrl('http:/');
  });

  it('rejects a scheme-only string with no authority or path (http://)', () => {
    // `new URL("http://")` throws because there is no valid host.
    expectInvalidUrl('http://');
  });

  it('rejects a path-only string (/api/v1)', () => {
    expectInvalidUrl('/api/v1');
  });

  it('rejects a string with spaces (not URL-encoded)', () => {
    expectInvalidUrl('https://api example.com');
  });

  it('rejects a human-readable label that is not a URL', () => {
    expectInvalidUrl('not-a-url');
  });

  it('rejects an arbitrary word string', () => {
    expectInvalidUrl('hello');
  });

  it('rejects a URL-looking string with a double-colon authority separator', () => {
    // "https::example.com" is not valid; `new URL` will throw.
    expectInvalidUrl('https::example.com');
  });

  it('error message embeds the raw value for all tested invalid strings', () => {
    // Parametric sweep — ensures the message template is stable across inputs.
    const invalidInputs = [
      'example.com',
      'not-a-url',
      '//example.com',
      '/api/v1',
      'hello',
      'http://',
    ];
    for (const raw of invalidInputs) {
      let caught: unknown;
      try {
        createStellarBillClient({ baseUrl: raw, fetch: makeStubFetch().fetch });
      } catch (err) {
        caught = err;
      }
      expect(caught, `expected StellarBillConfigError for raw="${raw}"`).toBeInstanceOf(
        StellarBillConfigError,
      );
      const e = caught as StellarBillConfigError;
      expect(e.message, `message should embed raw="${raw}"`).toContain(`"${raw}"`);
      expect(e.message, `message should contain "is not a valid URL" for raw="${raw}"`).toContain(
        'is not a valid URL',
      );
    }
  });

  // ----- No state leakage -----
  it('does not produce a client object when the URL is invalid', () => {
    // Ensure the throw propagates synchronously and createStellarBillClient
    // never returns a value for an invalid URL.
    let client: ReturnType<typeof createStellarBillClient> | undefined;
    try {
      client = createStellarBillClient({ baseUrl: 'not-a-url', fetch: makeStubFetch().fetch });
    } catch {
      // expected
    }
    expect(client).toBeUndefined();
  });

  it('does not invoke fetch when baseUrl is invalid', () => {
    // The stub must never be called; validateBaseUrl throws before fetch is
    // used. This verifies no request is attempted with the invalid URL.
    const stub = makeStubFetch();
    try {
      createStellarBillClient({ baseUrl: 'not-a-url', fetch: stub.fetch });
    } catch {
      // expected
    }
    expect(stub.callCount()).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// 2. Valid URL — success path (neighboring branch)
//    Verifies the contrast: valid → accepted, client constructed, no error.
// ---------------------------------------------------------------------------
describe('validateBaseUrl — valid URL success path', () => {
  it('accepts a canonical https URL', () => {
    const stub = makeStubFetch();
    let client: ReturnType<typeof createStellarBillClient> | undefined;
    expect(() => {
      client = createStellarBillClient({ baseUrl: 'https://api.stellabill.com', fetch: stub.fetch });
    }).not.toThrow();
    expect(client).toBeDefined();
  });

  it('accepts http://localhost (development base URL)', () => {
    const stub = makeStubFetch();
    expect(() =>
      createStellarBillClient({ baseUrl: 'http://localhost:8080', fetch: stub.fetch }),
    ).not.toThrow();
  });

  it('accepts a URL with a trailing slash — strips it from the base', async () => {
    const stub = makeStubFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com/', fetch: stub.fetch });
    // getHealth is the simplest request; the composed URL must not have doubled slashes.
    const r = await sdk.getHealth();
    expect(r.requestUrl).not.toMatch(/\/\//);
    // requestUrl should contain the correct path segment without a duplicated slash.
    expect(r.requestUrl).toContain('/api/health');
  });

  it('accepts a URL with a sub-path', () => {
    const stub = makeStubFetch();
    expect(() =>
      createStellarBillClient({ baseUrl: 'https://api.example.com/v2', fetch: stub.fetch }),
    ).not.toThrow();
  });

  it('accepts a URL with query parameters (valid for new URL() parsing)', () => {
    // new URL('https://api.example.com?env=test') succeeds; baseUrl is valid.
    const stub = makeStubFetch();
    expect(() =>
      createStellarBillClient({ baseUrl: 'https://api.example.com?env=test', fetch: stub.fetch }),
    ).not.toThrow();
  });

  it('does not throw StellarBillConfigError for valid HTTPS URLs', () => {
    const validUrls = [
      'https://api.stellabill.com',
      'https://api.example.com',
      'https://api.example.com/v1',
      'http://localhost',
      'http://localhost:8080',
      'http://127.0.0.1:8080',
    ];
    for (const url of validUrls) {
      expect(
        () => createStellarBillClient({ baseUrl: url, fetch: makeStubFetch().fetch }),
        `should not throw for "${url}"`,
      ).not.toThrow(StellarBillConfigError);
    }
  });
});

// ---------------------------------------------------------------------------
// 3. Boundary inputs — all three rejection branches in validateBaseUrl
//    Ensures the branches are individually stable and not accidentally merged.
// ---------------------------------------------------------------------------
describe('validateBaseUrl — boundary inputs across all rejection branches', () => {
  // Branch 1: undefined / null → 'baseUrl is required'
  it('throws "baseUrl is required" for undefined', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: undefined as unknown as string, fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  it('throws "baseUrl is required" for null', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: null as unknown as string, fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl is required');
  });

  // Branch 2: non-string or empty/whitespace → 'baseUrl must be a non-empty string'
  it('throws "baseUrl must be a non-empty string" for empty string', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: '', fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl must be a non-empty string');
  });

  it('throws "baseUrl must be a non-empty string" for whitespace-only string', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: '   ', fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl must be a non-empty string');
  });

  it('throws "baseUrl must be a non-empty string" for a number coerced as unknown', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: 42 as unknown as string, fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl must be a non-empty string');
  });

  // Branch 3 (Issue #870 target) — 'baseUrl "${raw}" is not a valid URL'
  it('throws the invalid-URL message (not the non-empty-string message) for a non-empty invalid URL', () => {
    // 'example.com' is a non-empty string — it clears Branch 2's guard —
    // but it is not a valid URL, so it must reach Branch 3.
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: 'example.com', fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    const e = caught as StellarBillConfigError;
    // Must NOT be the Branch 2 message.
    expect(e.message).not.toBe('baseUrl must be a non-empty string');
    // Must be the Branch 3 message.
    expect(e.message).toBe('baseUrl "example.com" is not a valid URL');
  });

  // Confirm branch messages are mutually exclusive.
  it('does not produce the "required" message for a non-empty invalid URL', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: 'not-a-url', fetch: makeStubFetch().fetch });
    } catch (err) {
      caught = err;
    }
    const e = caught as StellarBillConfigError;
    expect(e.message).not.toBe('baseUrl is required');
    expect(e.message).not.toBe('baseUrl must be a non-empty string');
    expect(e.message).toBe('baseUrl "not-a-url" is not a valid URL');
  });
});
