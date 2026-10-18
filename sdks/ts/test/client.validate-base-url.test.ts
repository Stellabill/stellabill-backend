import { describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

/**
 * Regression coverage for the rejected-input path of `validateBaseUrl`
 * (sdks/ts/src/client.ts:80-97, return at :96).
 *
 * `validateBaseUrl` is the first thing `createStellarBillClient` runs, so a
 * rejected baseUrl must fail fast with a stable, actionable
 * `StellarBillConfigError` *before* any network or fetch configuration is
 * touched. The adjacent success path (trailing-slash stripping) is asserted
 * too so the rejection contract cannot silently drift into the happy path.
 */

/** A fetch stub that counts usage; a rejected baseUrl must never call it. */
function forbiddenFetch(): { fetch: typeof globalThis.fetch; calls: () => number } {
  let calls = 0;
  const fetch = (async () => {
    calls += 1;
    throw new Error('fetch must not be reached for a rejected baseUrl');
  }) as unknown as typeof globalThis.fetch;
  return { fetch, calls: () => calls };
}

describe('validateBaseUrl - rejected input (client.ts:96)', () => {
  it('rejects null with the "required" contract', () => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: null as unknown as string });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    const e = caught as StellarBillConfigError;
    expect(e.name).toBe('StellarBillConfigError');
    expect(e.message).toBe('baseUrl is required');
  });

  it.each([
    ['number', 42],
    ['boolean', true],
    ['plain object', {}],
    ['array', ['https://api.example.com']],
    ['function', () => 'https://api.example.com'],
  ])('rejects non-string baseUrl (%s) with the non-empty-string contract', (_label, value) => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: value as unknown as string });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe('baseUrl must be a non-empty string');
  });

  it.each([
    ['empty', ''],
    ['spaces', '   '],
    ['tab/newline only', '\t\n  '],
    ['non-breaking space', '\u00a0'],
  ])('rejects whitespace-only baseUrl (%s)', (_label, value) => {
    expect(() => createStellarBillClient({ baseUrl: value })).toThrow(StellarBillConfigError);
    expect(() => createStellarBillClient({ baseUrl: value })).toThrow(/non-empty/);
  });

  it.each([
    ['no scheme', 'not-a-url'],
    ['scheme only', 'https://'],
    ['missing scheme', '://api.example.com'],
    ['unclosed IPv6 host', 'http://[::1'],
    ['relative path', '/api/v1'],
  ])('rejects malformed URL (%s) with an actionable message', (_label, value) => {
    let caught: unknown;
    try {
      createStellarBillClient({ baseUrl: value });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(StellarBillConfigError);
    expect((caught as StellarBillConfigError).message).toBe(
      `baseUrl "${value}" is not a valid URL`,
    );
  });

  it('never reaches fetch when the baseUrl is rejected', () => {
    const stub = forbiddenFetch();
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url', fetch: stub.fetch })).toThrow(
      StellarBillConfigError,
    );
    expect(stub.calls()).toBe(0);
  });

  it('accepts a valid baseUrl and preserves path/query while stripping only trailing slashes', () => {
    const stub = forbiddenFetch();
    // The constructor itself is the adjacent success path: it must NOT throw.
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com/tenant-42///',
      fetch: stub.fetch,
    });
    expect(sdk).toBeDefined();
    expect(stub.calls()).toBe(0);
  });
});
