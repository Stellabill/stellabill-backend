import { afterEach, describe, expect, it, vi } from 'vitest';

import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';
import { SDK_VERSION } from '../src/version.js';

// ─── mock fetch ─────────────────────────────────────────────────────────────

interface SentRequest {
  url: string;
  headers: Record<string, string>;
}

/** Mock fetch that records the fully-resolved request headers. */
function recordingFetch(body: unknown = { status: 'ok' }, status = 200): {
  fetch: typeof globalThis.fetch;
  sent: SentRequest[];
} {
  const sent: SentRequest[] = [];
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers: Record<string, string> = {};
    if (input instanceof Request) {
      input.headers.forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    if (init?.headers) {
      new Headers(init.headers).forEach((value, key) => {
        headers[key.toLowerCase()] = value;
      });
    }
    sent.push({ url: input instanceof Request ? input.url : String(input), headers });
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    });
  });
  return { fetch: fetch as unknown as typeof globalThis.fetch, sent };
}

/** Loosely-typed access to the raw openapi-fetch client for per-call header params. */
type RawGet = (
  path: string,
  init?: { params?: { header?: Record<string, string> } },
) => Promise<unknown>;

afterEach(() => {
  vi.restoreAllMocks();
});

// ─── auth / header injection branch (client.ts authMiddleware.onRequest) ────

describe('createStellarBillClient - request header injection', () => {
  it('rejects a caller-supplied Authorization header regardless of casing', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: 'sdk-token',
      headers: { aUtHoRiZaTiOn: 'Bearer attacker-controlled' },
      fetch,
    });

    await sdk.getHealth();

    expect(sent[0]!.headers['authorization']).toBe('Bearer sdk-token');
  });

  it('drops a caller-supplied Authorization header even when no token is configured', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { Authorization: 'Bearer attacker-controlled' },
      fetch,
    });

    await sdk.getHealth();

    expect(sent[0]!.headers['authorization']).toBeUndefined();
  });

  it('never sends an Authorization header for a whitespace-only token', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      token: '   ',
      fetch,
    });

    await sdk.getHealth();

    expect(sent[0]!.headers['authorization']).toBeUndefined();
  });

  it('skips static headers whose runtime value is not a non-empty string', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      // Runtime-invalid values that the `typeof v === 'string'` guard must reject.
      headers: {
        'X-Num': 123 as unknown as string,
        'X-Obj': {} as unknown as string,
        'X-Empty': '',
        'X-Ok': 'kept',
      },
      fetch,
    });

    await sdk.getHealth();

    expect(sent[0]!.headers['x-num']).toBeUndefined();
    expect(sent[0]!.headers['x-obj']).toBeUndefined();
    expect(sent[0]!.headers['x-empty']).toBeUndefined();
    expect(sent[0]!.headers['x-ok']).toBe('kept');
  });

  it('normalizes static header names to lowercase', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Mixed-Case-Name': 'v' },
      fetch,
    });

    await sdk.getHealth();

    expect(sent[0]!.headers['x-mixed-case-name']).toBe('v');
    expect(sent[0]!.headers['X-Mixed-Case-Name']).toBeUndefined();
  });

  it('sends the SDK user-agent built from the current SDK version', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });

    await sdk.getHealth();

    expect(sent[0]!.headers['user-agent']).toBe(`@stellabill/sdk/${SDK_VERSION} node/${process.versions.node}`);
  });

  it('does not overwrite a caller-provided user-agent already on the request', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'tok', fetch });

    await (sdk.raw.GET as unknown as RawGet)('/api/health', {
      params: { header: { 'user-agent': 'caller-agent/1.0' } },
    });

    expect(sent[0]!.headers['user-agent']).toBe('caller-agent/1.0');
    expect(sent[0]!.headers['authorization']).toBe('Bearer tok');
  });

  it('does not overwrite a static header the caller already set on the request', async () => {
    const { fetch, sent } = recordingFetch();
    const sdk = createStellarBillClient({
      baseUrl: 'https://api.example.com',
      headers: { 'X-Custom-Trace': 'sdk-static' },
      fetch,
    });

    await (sdk.raw.GET as unknown as RawGet)('/api/health', {
      params: { header: { 'x-custom-trace': 'caller-param' } },
    });

    expect(sent[0]!.headers['x-custom-trace']).toBe('caller-param');
  });

  it('rejects an invalid baseUrl before any request is attempted', async () => {
    const { fetch, sent } = recordingFetch();

    expect(() => createStellarBillClient({ baseUrl: 'not-a-url', fetch })).toThrow(
      StellarBillConfigError,
    );
    expect(sent).toHaveLength(0);
  });
});
