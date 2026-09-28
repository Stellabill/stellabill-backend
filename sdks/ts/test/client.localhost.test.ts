/**
 * Boundary-condition coverage for the `} catch {` branch in
 * `src/client.ts` — the `isLocalhost` helper that decides whether the
 * insecure-`http://` warning is emitted for a given `baseUrl`.
 *
 * Two layers are exercised:
 *   1. `isLocalhost` directly (hostname boundaries + the URL parse-failure
 *      fallback branch), now exported from `src/client.ts` for that purpose.
 *   2. `createStellarBillClient` end to end, asserting the observable
 *      `console.warn` decision for each boundary.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, isLocalhost } from '../src/client.js';
import { StellarBillConfigError } from '../src/errors.js';

function okFetch(): typeof fetch {
  return vi.fn(
    async () =>
      new Response(JSON.stringify({ status: 'ok', service: 'stellabill-backend' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
  ) as unknown as typeof fetch;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('isLocalhost — reachable hostname boundaries', () => {
  it.each([
    ['http://localhost', true],
    ['http://localhost/', true],
    ['http://localhost:8080', true],
    ['https://localhost', true],
    ['http://LOCALHOST:8080', true],
    ['http://LocalHost', true],
    ['http://127.0.0.1', true],
    ['http://127.0.0.1/', true],
    ['http://127.0.0.1:3000', true],
    ['https://127.0.0.1', true],
    ['http:/localhost', true],
    ['  http://localhost  ', true],
    ['http://example.com', false],
    ['https://example.com', false],
    ['http://localhost.example.com', false],
    ['http://localhost.evil.com', false],
    ['http://127.0.0.1.nip.io', false],
    ['http://127.0.0.2', false],
    ['http://192.168.0.1', false],
    ['http://0.0.0.0', false],
    ['http://[::1]:3000', false],
  ])('isLocalhost(%s) === %s', (url, expected) => {
    expect(isLocalhost(url)).toBe(expected);
  });
});

describe('isLocalhost — URL parse-failure branch (client.ts:103)', () => {
  it.each(['', 'not-a-url', 'http://', '://missing-scheme'])(
    'returns false (never throws) for the unparseable value %s',
    (value) => {
      expect(() => isLocalhost(value)).not.toThrow();
      expect(isLocalhost(value)).toBe(false);
    },
  );

  it('is deterministic across repeated calls', () => {
    expect(isLocalhost('not-a-url')).toBe(isLocalhost('not-a-url'));
    expect(isLocalhost('not-a-url')).toBe(false);
  });
});

describe('createStellarBillClient — insecure-url warning boundaries', () => {
  it.each([
    ['http://example.com', true],
    ['http://localhost.example.com', true],
    ['http://localhost.evil.com', true],
    ['http://127.0.0.1.nip.io', true],
    ['http://127.0.0.2', true],
    ['http://[::1]:3000', true],
    ['https://example.com', false],
    ['https://127.0.0.2', false],
    ['http://localhost:8080', false],
    ['http://127.0.0.1:8080', false],
    ['https://localhost', false],
  ])('for %s -> warns=%s', async (baseUrl, shouldWarn) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const sdk = createStellarBillClient({ baseUrl, fetch: okFetch() });
    await sdk.getHealth();

    if (shouldWarn) {
      expect(warn).toHaveBeenCalledTimes(1);
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('Insecure baseUrl'));
    } else {
      expect(warn).not.toHaveBeenCalled();
    }
  });

  it('rejects an unparseable baseUrl before isLocalhost can ever see it', () => {
    // The warning decision calls isLocalhost with the *validated* URL, so the
    // parse-failure branch is defensive: an invalid baseUrl never reaches it.
    expect(() => createStellarBillClient({ baseUrl: 'not-a-url', fetch: okFetch() })).toThrow(
      StellarBillConfigError,
    );
    expect(() => createStellarBillClient({ baseUrl: 'http://', fetch: okFetch() })).toThrow(
      /not a valid URL/,
    );
  });

  it('does not warn twice for the same client configuration', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});

    const sdk = createStellarBillClient({ baseUrl: 'http://127.0.0.2:8080', fetch: okFetch() });
    await sdk.getHealth();
    await sdk.getHealth();

    expect(warn).toHaveBeenCalledTimes(1);
  });
});
