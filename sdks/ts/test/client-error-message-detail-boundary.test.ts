import { describe, expect, it } from 'vitest';

import {
  assertOk,
  StellarBillError,
  type ApiErrorBody,
  type SdkResult,
} from '../src/index.js';

/**
 * Focused boundary coverage for the detail string built in `makeErrorMessage`
 * (`sdks/ts/src/client.ts`, issue #910):
 *
 *   const detail = body?.message ?? body?.error ?? `HTTP ${status}`;
 *   return `${method} ${url} failed (${status}): ${detail}`;
 *
 * The `??` chain is the branch: it only falls through on `null`/`undefined`, so
 * any other value — including `''`, `0`, or `false` — is a *present* detail and
 * must win. This file pins that boundary (so the branch cannot silently start
 * treating falsy values as "missing") and the exact framing around it, using
 * `assertOk`, which is the public entry point that reaches this code for a
 * non-2xx envelope.
 *
 * The complementary happy-path/fallback table lives in
 * `error-message-contract.test.ts`; this file deliberately targets the
 * falsy-but-present and non-string edges.
 */

function envelope(overrides: Partial<SdkResult<unknown>> = {}): SdkResult<unknown> {
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

async function rejectionMessage(result: SdkResult<unknown>): Promise<string> {
  try {
    await assertOk(result);
  } catch (err) {
    expect(err).toBeInstanceOf(StellarBillError);
    return (err as StellarBillError).message;
  }
  throw new Error('expected assertOk to reject');
}

describe('makeErrorMessage detail boundary (issue #910)', () => {
  it('keeps a falsy-but-present message (0) instead of falling through to error', async () => {
    const message = await rejectionMessage(
      envelope({ error: { message: 0, error: 'should-not-win' } as unknown as ApiErrorBody }),
    );
    expect(message).toBe('GET /api/v1/plans failed (400): 0');
  });

  it('keeps a falsy-but-present message (false) instead of falling through', async () => {
    const message = await rejectionMessage(
      envelope({ error: { message: false, error: 'should-not-win' } as unknown as ApiErrorBody }),
    );
    expect(message).toBe('GET /api/v1/plans failed (400): false');
  });

  it('stringifies a whitespace-only message rather than trimming it away', async () => {
    const message = await rejectionMessage(
      envelope({ error: { message: '   ', error: 'should-not-win' } as ApiErrorBody }),
    );
    expect(message).toBe('GET /api/v1/plans failed (400):    ');
  });

  it('stringifies a non-string error detail when message is absent', async () => {
    const message = await rejectionMessage(
      envelope({ error: { error: 404 } as unknown as ApiErrorBody }),
    );
    expect(message).toBe('GET /api/v1/plans failed (400): 404');
  });

  it('falls back to the HTTP status only when both details are null/undefined', async () => {
    const missing = await rejectionMessage(
      envelope({ error: { message: undefined, error: undefined } as ApiErrorBody, status: 500 }),
    );
    expect(missing).toBe('GET /api/v1/plans failed (500): HTTP 500');

    const nullish = await rejectionMessage(
      envelope({ error: { message: null, error: null } as unknown as ApiErrorBody, status: 502 }),
    );
    expect(nullish).toBe('GET /api/v1/plans failed (502): HTTP 502');
  });

  it('frames method, url and status verbatim, including unusual characters', async () => {
    const message = await rejectionMessage(
      envelope({
        error: { message: 'bad' } as ApiErrorBody,
        status: 0,
        requestMethod: 'PATCH',
        requestUrl: '/api/v1/keys/a b?x=1&y=2',
      }),
    );
    expect(message).toBe('PATCH /api/v1/keys/a b?x=1&y=2 failed (0): bad');
  });

  it('is deterministic: the same rejected envelope yields the same message every time', async () => {
    const result = envelope({
      error: { message: 17, error: 'ignored' } as unknown as ApiErrorBody,
    });
    const first = await rejectionMessage(result);
    const second = await rejectionMessage(result);
    expect(first).toBe(second);
    expect(first).toBe('GET /api/v1/plans failed (400): 17');
  });
});
