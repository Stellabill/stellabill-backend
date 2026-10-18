import { describe, expect, it, vi } from 'vitest';

import { safeParseErrorBody } from '../src/index.js';

describe('safeParseErrorBody - rejected text input path', () => {
  it('returns undefined when content-type is missing', async () => {
    const r = new Response('{}', { status: 400 });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when content-type is not JSON', async () => {
    const r = new Response('oops', { status: 400, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when body is empty', async () => {
    const r = new Response('', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined on invalid JSON', async () => {
    const r = new Response('not-json', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is a string', async () => {
    const r = new Response('"a string"', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is an array', async () => {
    const r = new Response('[1,2,3]', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when parsed value is null', async () => {
    const r = new Response('null', { status: 400, headers: { 'content-type': 'application/json' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when status 200 with non-JSON content', async () => {
    const r = new Response('', { status: 200, headers: { 'content-type': 'text/plain' } });
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });

  it('returns undefined when res.text() throws', async () => {
    const r = new Response('ok', { status: 400, headers: { 'content-type': 'application/json' } });
    vi.spyOn(r, 'text').mockRejectedValueOnce(new Error('boom'));
    expect(await safeParseErrorBody(r)).toBeUndefined();
  });
});
