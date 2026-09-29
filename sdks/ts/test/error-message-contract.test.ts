import { describe, expect, it, vi } from 'vitest';

import {
  assertOk,
  createStellarBillClient,
  StellarBillError,
  type ApiErrorBody,
  type SdkResult,
} from '../src/index.js';

/**
 * Regression coverage for the rejected-input detail string built in
 * `makeErrorMessage` (`sdks/ts/src/client.ts:125-126`).
 *
 * `const detail = body?.message ?? body?.error ?? `HTTP ${status}`;` is the
 * only place a caller-visible failure reason is derived, and it is reached from
 * two independent paths:
 *
 *  1. `assertOk(result)` for a non-2xx envelope, and
 *  2. the `throwOnError: true` shortcut inside `createStellarBillClient`.
 *
 * The tests below pin the fallback chain itself — including the `??` semantics
 * that make an empty or non-string `message` win over `error`, and the cases
 * where a rejected body leaves *no* message at all — plus the surrounding
 * `"<METHOD> <url> failed (<status>): <detail>"` framing on both paths.
 */

function envelope(
  overrides: Partial<SdkResult<unknown>> = {},
): SdkResult<unknown> {
  return {
    data: undefined,
    status: 400,
    error: undefined,
    response: new Response(null, { status: 400 }),
    requestMethod: 'GET',
    requestUrl: '/api/v1/plans',
    ...overrides,
  };
}

function messageFrom(rejection: unknown): string {
  expect(rejection).toBeInstanceOf(StellarBillError);
  return (rejection as StellarBillError).message;
}

/** Run `assertOk` for a rejected envelope and return the thrown message. */
async function assertOkMessage(result: SdkResult<unknown>): Promise<string> {
  try {
    await assertOk(result);
  } catch (err) {
    return messageFrom(err);
  }
  throw new Error('expected assertOk to reject');
}

describe('makeErrorMessage - detail fallback chain', () => {
  it('prefers body.message when it is a string', async () => {
    const message = await assertOkMessage(
      envelope({ error: { message: 'quota exceeded', error: 'ignored', code: 'quota' } }),
    );

    expect(message).toBe('GET /api/v1/plans failed (400): quota exceeded');
  });

  it('falls back to body.error when message is absent', async () => {
    const message = await assertOkMessage(
      envelope({ error: { error: 'invalid_cursor' } as ApiErrorBody }),
    );

    expect(message).toBe('GET /api/v1/plans failed (400): invalid_cursor');
  });

  it('falls back to body.error when message is explicitly null', async () => {
    const message = await assertOkMessage(
      envelope({ error: { message: null, error: 'bad request' } as unknown as ApiErrorBody }),
    );

    expect(message).toBe('GET /api/v1/plans failed (400): bad request');
  });

  it('falls back to the HTTP status when both message and error are null', async () => {
    const message = await assertOkMessage(
      envelope({ error: { message: null, error: null } as unknown as ApiErrorBody, status: 503 }),
    );

    expect(message).toBe('GET /api/v1/plans failed (503): HTTP 503');
  });

  it('falls back to the HTTP status when the body is undefined', async () => {
    const message = await assertOkMessage(envelope({ error: undefined, status: 502 }));

    expect(message).toBe('GET /api/v1/plans failed (502): HTTP 502');
  });

  it('falls back to the HTTP status for an empty object body', async () => {
    const message = await assertOkMessage(envelope({ error: {} as ApiErrorBody, status: 418 }));

    expect(message).toBe('GET /api/v1/plans failed (418): HTTP 418');
  });

  it('keeps an empty-string message rather than falling through to error', async () => {
    // `??` only falls back on null/undefined, so "" wins over a usable `error`.
    const message = await assertOkMessage(
      envelope({ error: { message: '', error: 'should not be used' } as ApiErrorBody }),
    );

    expect(message).toBe('GET /api/v1/plans failed (400): ');
  });

  it('stringifies a non-string message instead of rejecting it', async () => {
    const message = await assertOkMessage(
      envelope({ error: { message: 42, error: 'ignored' } as unknown as ApiErrorBody }),
    );

    expect(message).toBe('GET /api/v1/plans failed (400): 42');
  });

  it('preserves the exact framing around method, url and status', async () => {
    const message = await assertOkMessage(
      envelope({
        error: { message: 'gone' } as ApiErrorBody,
        status: 404,
        requestMethod: 'POST',
        requestUrl: '/api/subscriptions/abc%2Fdef',
      }),
    );

    expect(message).toBe('POST /api/subscriptions/abc%2Fdef failed (404): gone');
  });

  it('does not treat a returned value as an error', async () => {
    const payload = { status: 'ok' };
    await expect(assertOk(envelope({ data: payload, status: 200 }))).resolves.toBe(payload);
  });

  it('rejects a 2xx envelope whose data is undefined', async () => {
    const rejection = await assertOk(envelope({ data: undefined, status: 204 })).catch((e) => e);

    expect(messageFrom(rejection)).toBe(
      'GET /api/v1/plans returned 2xx with empty body',
    );
  });
});

describe('makeErrorMessage - rejected error bodies from a real response', () => {
  /**
   * `wrap()` only attaches `error` when openapi-fetch produced an object; an
   * array or scalar payload arrives as `undefined`, which must degrade to the
   * `HTTP <status>` detail rather than throwing.
   */
  it.each([
    ['null', 'null'],
    ['number', '12'],
    ['boolean', 'false'],
    ['quoted string', '"nope"'],
    ['truncated JSON', '{"message":'],
  ])('degrades to HTTP <status> for a %s error body', async (_label, body) => {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      fetch: vi.fn(async () => new Response(body, { status: 400, headers: { 'content-type': 'application/json' } })) as unknown as typeof globalThis.fetch,
    });

    const result = await client.getHealth();

    expect(result.error).toBeUndefined();
    await expect(assertOk(result)).rejects.toThrow('GET /api/health failed (400): HTTP 400');
  });

  it('degrades to HTTP <status> for an array error body too', async () => {
    // `wrap()` passes the array through as `error` (it only checks
    // `typeof === "object"`), but an array carries no `message`/`error` key, so
    // the detail still falls back to the status.
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      fetch: vi.fn(
        async () =>
          new Response('[{"message":"nope"}]', {
            status: 400,
            headers: { 'content-type': 'application/json' },
          }),
      ) as unknown as typeof globalThis.fetch,
    });

    const result = await client.getHealth();

    expect(result.error).toEqual([{ message: 'nope' }]);
    await expect(assertOk(result)).rejects.toThrow('GET /api/health failed (400): HTTP 400');
  });

  it('uses the parsed message for a valid JSON error body', async () => {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      fetch: vi.fn(
        async () =>
          new Response(JSON.stringify({ message: 'plans unavailable' }), {
            status: 503,
            headers: { 'content-type': 'application/json' },
          }),
      ) as unknown as typeof globalThis.fetch,
    });

    const result = await client.listPlans();

    expect(result.status).toBe(503);
    expect(result.error).toEqual({ message: 'plans unavailable' });
    await expect(assertOk(result)).rejects.toThrow(
      'GET /api/v1/plans failed (503): plans unavailable',
    );
  });
});

describe('makeErrorMessage - throwOnError path uses the same detail', () => {
  it('throws the identical message that assertOk would produce', async () => {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      throwOnError: true,
      fetch: vi.fn(
        async () =>
          new Response(JSON.stringify({ error: 'invalid_cursor' }), {
            status: 400,
            headers: { 'content-type': 'application/json' },
          }),
      ) as unknown as typeof globalThis.fetch,
    });

    const rejection = await client.listPlans({ cursor: 'x' }).catch((e) => e);

    expect(messageFrom(rejection)).toBe(
      'GET /api/v1/plans failed (400): invalid_cursor',
    );
    expect((rejection as StellarBillError).body).toEqual({ error: 'invalid_cursor' });
  });

  it('throws with the HTTP-status detail when the error body is not JSON', async () => {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      throwOnError: true,
      fetch: vi.fn(
        async () => new Response('<html>oops</html>', { status: 500, headers: { 'content-type': 'text/html' } }),
      ) as unknown as typeof globalThis.fetch,
    });

    const rejection = await client.getHealth().catch((e) => e);

    expect(messageFrom(rejection)).toBe('GET /api/health failed (500): HTTP 500');
  });

  it('leaves successful 2xx responses untouched when throwOnError is enabled', async () => {
    const client = createStellarBillClient({
      baseUrl: 'https://api.stellabill.com',
      throwOnError: true,
      fetch: vi.fn(
        async () =>
          new Response(JSON.stringify({ status: 'ok' }), {
            status: 200,
            headers: { 'content-type': 'application/json' },
          }),
      ) as unknown as typeof globalThis.fetch,
    });

    await expect(client.getHealth()).resolves.toMatchObject({
      data: { status: 'ok' },
      status: 200,
    });
  });
});
