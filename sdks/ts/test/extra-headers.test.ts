/**
 * Regression tests for the `options.headers` accepted-input branch
 * at `sdks/ts/src/client.ts:169`.
 *
 * The branch under test:
 *   if (options.headers) {
 *     for (const [k, v] of Object.entries(options.headers)) {
 *       const lower = k.toLowerCase();
 *       if (lower === 'authorization') continue;
 *       if (typeof v === 'string' && v.length > 0) {
 *         extraHeaders[lower] = v;
 *       }
 *     }
 *   }
 *
 * Purpose: verify that representative valid `extraHeaders`/`options.headers`
 * input reaches this branch and produces the expected observable SDK/request
 * behavior, including the truthy and falsy paths.
 *
 * Closes #926.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient } from '../src/index.js';

// ---------------------------------------------------------------------------
// Test infrastructure (mirrors pattern in client.test.ts)
// ---------------------------------------------------------------------------

type FetchCall = {
  url: string;
  init: RequestInit | undefined;
  inputHeaders: Record<string, string>;
  initHeaders: Record<string, string>;
};

function toUrl(input: Parameters<typeof fetch>[0]): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.toString();
  if (input instanceof Request) return input.url;
  return String(input);
}

function headersFromInit(init?: RequestInit): Record<string, string> {
  const out: Record<string, string> = {};
  if (!init?.headers) return out;
  new Headers(init.headers).forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

/** Merge Request-object headers with init headers (Request takes precedence). */
function callHeaders(call: FetchCall): Record<string, string> {
  return { ...call.initHeaders, ...call.inputHeaders };
}

/**
 * Returns a mock fetch that responds once with the supplied body and records
 * every call together with its headers.
 */
function mockFetchOnce(
  body: unknown,
  init: { status?: number; contentType?: string } = {},
): { fetch: typeof globalThis.fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const status = init.status ?? 200;
  const contentType = init.contentType ?? 'application/json';
  const text =
    body === undefined
      ? ''
      : typeof body === 'string'
        ? body
        : JSON.stringify(body);
  const res = new Response(text, {
    status,
    headers: { 'content-type': contentType },
  });
  const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
    const inputHeaders: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        inputHeaders[key.toLowerCase()] = value;
      });
    }
    calls.push({
      url: toUrl(input),
      init: initArg as RequestInit | undefined,
      inputHeaders,
      initHeaders: headersFromInit(initArg as RequestInit | undefined),
    });
    return res;
  });
  return { fetch, calls };
}

// Standard 200 health body used by many tests so inline assertions stay clean.
const OK_HEALTH = { status: 'ok', service: 'stellarbill-backend' } as const;

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// §1 — Representative valid custom header (primary regression test)
// ---------------------------------------------------------------------------

describe('options.headers — representative valid input reaches outgoing request', () => {
  it('single regression header X-Test-Request is present in the request with preserved value', async () => {
    // This is the primary regression test for issue #926.
    // It constructs the SDK with one representative extra header, fires an
    // operation, and asserts the header appears on the outgoing fetch call.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'regression-test' },
      fetch,
    });

    const result = await sdk.getHealth();

    // The header must be present in the actual request.
    expect(calls).toHaveLength(1);
    const headers = callHeaders(calls[0]!);
    expect(headers['x-test-request']).toBe('regression-test');

    // The SDK must still return the expected parsed response.
    expect(result.status).toBe(200);
    expect(result.data?.status).toBe('ok');
    expect(result.requestMethod).toBe('GET');
  });

  it('options.headers being truthy (non-empty object) actually enters the branch and populates extraHeaders', async () => {
    // Exercises the truthy arm: options.headers is defined and non-empty so
    // the for-of loop runs and extraHeaders gains the caller-supplied entry.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Regression-Branch': 'entered' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    // If the branch was NOT entered, this key would be absent.
    expect(headers['x-regression-branch']).toBe('entered');
  });
});

// ---------------------------------------------------------------------------
// §2 — No extra-headers path (false branch of `if (options.headers)`)
// ---------------------------------------------------------------------------

describe('options.headers — absent / false-branch behavior', () => {
  it('omitting options.headers entirely still makes a valid request (false branch)', async () => {
    // The absence of options.headers must not prevent requests from succeeding.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch,
      // No `headers` property at all.
    });

    const result = await sdk.getHealth();

    expect(calls).toHaveLength(1);
    const headers = callHeaders(calls[0]!);

    // The SDK must still inject user-agent even with no caller headers.
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);

    // No unexpected custom header appears.
    const customHeaderKeys = Object.keys(headers).filter(
      (k) => k !== 'user-agent' && k !== 'authorization',
    );
    expect(customHeaderKeys).toHaveLength(0);

    expect(result.status).toBe(200);
    expect(result.data?.status).toBe('ok');
  });

  it('explicitly passing undefined for options.headers behaves like omitting it', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: undefined,
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);

    // No custom key present beyond user-agent.
    const extra = Object.keys(headers).filter(
      (k) => k !== 'user-agent' && k !== 'authorization',
    );
    expect(extra).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// §3 — Empty object boundary
// ---------------------------------------------------------------------------

describe('options.headers — empty object boundary', () => {
  it('empty headers object {} is accepted without error', async () => {
    // {} is a valid Record<string,string> with zero entries.
    // The for-of loop runs zero iterations; no custom header is added.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {},
      fetch,
    });

    const result = await sdk.getHealth();

    expect(calls).toHaveLength(1);
    const headers = callHeaders(calls[0]!);

    // user-agent is always present (added unconditionally after the branch).
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);

    // No custom header keys beyond the unconditionally-added user-agent.
    const customKeys = Object.keys(headers).filter(
      (k) => k !== 'user-agent' && k !== 'authorization',
    );
    expect(customKeys).toHaveLength(0);

    expect(result.status).toBe(200);
  });
});

// ---------------------------------------------------------------------------
// §4 — Multiple custom headers preserved independently
// ---------------------------------------------------------------------------

describe('options.headers — multiple headers preserved independently', () => {
  it('all provided custom headers appear in the request and keep their values', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'X-Tenant-ID': 'tenant-42',
        'X-Trace-ID': 'trace-abc-123',
        'X-Source': 'regression-suite',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-tenant-id']).toBe('tenant-42');
    expect(headers['x-trace-id']).toBe('trace-abc-123');
    expect(headers['x-source']).toBe('regression-suite');
  });

  it('multiple custom headers do not interfere with each other', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'X-First': 'first-value',
        'X-Second': 'second-value',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-first']).toBe('first-value');
    expect(headers['x-second']).toBe('second-value');
    // Both present simultaneously, no cross-contamination.
    expect(headers['x-first']).not.toBe(headers['x-second']);
  });
});

// ---------------------------------------------------------------------------
// §5 — Header key case normalization
// ---------------------------------------------------------------------------

describe('options.headers — key case normalization', () => {
  it('mixed-case header keys are lowercased before being sent', async () => {
    // The implementation calls k.toLowerCase(), so "X-Tenant-ID" in the
    // input must appear as "x-tenant-id" on the request.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'X-Tenant-ID': 'tenant-99',
        'X-UPPER': 'up',
        'x-lower': 'lo',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);

    // All three variants must be normalized to lowercase.
    expect(headers['x-tenant-id']).toBe('tenant-99');
    expect(headers['x-upper']).toBe('up');
    expect(headers['x-lower']).toBe('lo');
  });

  it('AUTHORIZATION (uppercase) is also blocked by the case-insensitive check', async () => {
    // The bypass check is: lower === 'authorization'. So both "Authorization"
    // and "AUTHORIZATION" must be blocked.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { AUTHORIZATION: 'Bearer injected' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// §6 — Custom headers coexist with authentication token
// ---------------------------------------------------------------------------

describe('options.headers — coexistence with authentication token', () => {
  it('custom headers and Bearer auth are both present simultaneously', async () => {
    // This is the key "headers do not displace auth" regression.
    // The branch must deposit custom headers WITHOUT removing the auth header
    // that the authMiddleware subsequently adds.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'secret-token',
      headers: { 'X-Tenant-ID': 'coexistence-test' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-tenant-id']).toBe('coexistence-test');
    expect(headers['authorization']).toBe('Bearer secret-token');
    // user-agent must also remain.
    expect(headers['user-agent']).toMatch(/^@stellabill\/sdk\//);
  });

  it('custom headers are present even when no auth token is configured', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      // No token.
      headers: { 'X-Correlation-ID': 'no-auth-scenario' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-correlation-id']).toBe('no-auth-scenario');
    expect(headers['authorization']).toBeUndefined();
  });

  it('user-agent is always present regardless of whether custom headers are supplied', async () => {
    // user-agent is added after the if (options.headers) block and must be
    // present whether or not caller headers were supplied.
    const withHeaders = mockFetchOnce(OK_HEALTH);
    const sdkWith = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Tag': 'with' },
      fetch: withHeaders.fetch,
    });
    await sdkWith.getHealth();

    const withoutHeaders = mockFetchOnce(OK_HEALTH);
    const sdkWithout = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      fetch: withoutHeaders.fetch,
    });
    await sdkWithout.getHealth();

    expect(callHeaders(withHeaders.calls[0]!)['user-agent']).toMatch(/^@stellabill\/sdk\//);
    expect(callHeaders(withoutHeaders.calls[0]!)['user-agent']).toMatch(/^@stellabill\/sdk\//);
  });
});

// ---------------------------------------------------------------------------
// §7 — Custom headers on non-getHealth operations
// ---------------------------------------------------------------------------

describe('options.headers — headers appear on all operations, not just getHealth', () => {
  it('custom header is present on listPlans request', async () => {
    const { fetch, calls } = mockFetchOnce({
      plans: [],
      pagination: { has_more: false },
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'listPlans-regression' },
      fetch,
    });

    await sdk.listPlans();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-test-request']).toBe('listPlans-regression');
    expect(calls[0]!.url).toContain('/api/v1/plans');
  });

  it('custom header is present on listSubscriptions request', async () => {
    const { fetch, calls } = mockFetchOnce({
      subscriptions: [],
      pagination: { has_more: false },
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'listSubs-regression' },
      fetch,
    });

    await sdk.listSubscriptions();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-test-request']).toBe('listSubs-regression');
    expect(calls[0]!.url).toContain('/api/subscriptions');
  });

  it('custom header is present on getSubscription request', async () => {
    const { fetch, calls } = mockFetchOnce({
      id: 'sub-1',
      plan_id: 'plan-1',
      customer: 'cust-1',
      status: 'active',
      amount: '1000',
      interval: 'monthly',
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'getSub-regression' },
      fetch,
    });

    await sdk.getSubscription('sub-1');

    const headers = callHeaders(calls[0]!);
    expect(headers['x-test-request']).toBe('getSub-regression');
    expect(calls[0]!.url).toContain('/api/subscriptions/sub-1');
  });

  it('custom header is present on inspectIdempotencyKey request', async () => {
    const { fetch, calls } = mockFetchOnce({
      key: 'key-abc',
      used_at: '2026-01-01T00:00:00Z',
      expires_at: '2026-01-02T00:00:00Z',
      status_code: 200,
      request_fingerprint: 'fp',
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'idempotency-regression' },
      fetch,
    });

    await sdk.inspectIdempotencyKey('key-abc');

    const headers = callHeaders(calls[0]!);
    expect(headers['x-test-request']).toBe('idempotency-regression');
    expect(calls[0]!.url).toContain('/api/v1/idempotency/');
  });

  it('custom headers are injected consistently across multiple sequential calls', async () => {
    // The extraHeaders object is closed over once at client construction.
    // Verify it is applied on every call, not just the first.
    const responses = [OK_HEALTH, OK_HEALTH] as const;
    let callCount = 0;
    const calls: FetchCall[] = [];
    const fetch: typeof globalThis.fetch = vi.fn(async (input, initArg) => {
      const inputHeaders: Record<string, string> = {};
      if (input instanceof Request) {
        input.headers.forEach((v, k) => {
          inputHeaders[k.toLowerCase()] = v;
        });
      }
      calls.push({
        url: typeof input === 'string' ? input : input instanceof URL ? input.toString() : (input as Request).url,
        init: initArg as RequestInit | undefined,
        inputHeaders,
        initHeaders: (() => {
          const out: Record<string, string> = {};
          if (!(initArg as RequestInit | undefined)?.headers) return out;
          new Headers((initArg as RequestInit).headers).forEach((v, k) => {
            out[k.toLowerCase()] = v;
          });
          return out;
        })(),
      });
      const body = responses[callCount++]!;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });

    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Per-Call': 'persistent' },
      fetch,
    });

    await sdk.getHealth();
    await sdk.getHealth();

    expect(calls).toHaveLength(2);
    expect(callHeaders(calls[0]!)['x-per-call']).toBe('persistent');
    expect(callHeaders(calls[1]!)['x-per-call']).toBe('persistent');
  });
});

// ---------------------------------------------------------------------------
// §8 — Empty-value header skip (the inner value guard)
// ---------------------------------------------------------------------------

describe('options.headers — empty-string value is not sent', () => {
  it('header with empty string value is skipped; others in the same object are kept', async () => {
    // Both the truthy branch AND the inner empty-value guard are covered.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        'X-Skip-Me': '',
        'X-Keep-Me': 'present',
        'X-Also-Skip': '',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['x-skip-me']).toBeUndefined();
    expect(headers['x-also-skip']).toBeUndefined();
    expect(headers['x-keep-me']).toBe('present');
  });
});

// ---------------------------------------------------------------------------
// §9 — Authorization bypass prevention (case-insensitive)
// ---------------------------------------------------------------------------

describe('options.headers — Authorization bypass prevention', () => {
  it('lowercase "authorization" key is blocked', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { authorization: 'Bearer sneaky' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    // No token configured, so authorization should be absent entirely.
    expect(headers['authorization']).toBeUndefined();
  });

  it('"authorization" key is blocked even when another valid header is also provided', async () => {
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: {
        authorization: 'Bearer injected',
        'X-Real-Header': 'safe',
      },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    expect(headers['authorization']).toBeUndefined();
    expect(headers['x-real-header']).toBe('safe');
  });

  it('SDK-managed Bearer auth overrides any caller attempt when token is set', async () => {
    // Even if caller somehow passed authorization, SDK's own token wins.
    const { fetch, calls } = mockFetchOnce(OK_HEALTH);
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'real-sdk-token',
      headers: { Authorization: 'Bearer attacker' },
      fetch,
    });

    await sdk.getHealth();

    const headers = callHeaders(calls[0]!);
    // The SDK's own token must be used, not the injected one.
    expect(headers['authorization']).toBe('Bearer real-sdk-token');
  });
});

// ---------------------------------------------------------------------------
// §10 — Response/result behavior is correct when custom headers are supplied
// ---------------------------------------------------------------------------

describe('options.headers — SDK response/result behavior is unaffected', () => {
  it('custom headers do not alter the parsed response shape', async () => {
    const { fetch } = mockFetchOnce({
      status: 'ok',
      service: 'stellarbill-backend',
    });
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'response-shape-check' },
      fetch,
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(200);
    expect(result.error).toBeUndefined();
    expect(result.data?.status).toBe('ok');
    expect(result.data?.service).toBe('stellarbill-backend');
    expect(result.requestMethod).toBe('GET');
    expect(result.requestUrl).toContain('/api/health');
  });

  it('custom headers do not suppress error responses', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Unauthorized', message: 'token missing', code: 'auth_unauthorized' },
      { status: 401 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Test-Request': 'error-path-check' },
      fetch,
    });

    const result = await sdk.getHealth();

    expect(result.status).toBe(401);
    expect(result.error?.code).toBe('auth_unauthorized');
    expect(result.data).toBeUndefined();
  });

  it('throwOnError + custom headers still throws on non-2xx', async () => {
    const { fetch } = mockFetchOnce(
      { error: 'Not Found', message: 'missing', code: 'not_found' },
      { status: 404 },
    );
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      throwOnError: true,
      headers: { 'X-Test-Request': 'throw-check' },
      fetch,
    });

    await expect(sdk.getHealth()).rejects.toMatchObject({ status: 404 });
  });
});
