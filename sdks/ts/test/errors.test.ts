import { afterEach, describe, expect, it, vi } from 'vitest';

import { StellarBillConfigError, StellarBillError } from '../src/index.js';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('StellarBillError', () => {
  it('stores all fields and instanceof works', () => {
    const err = new StellarBillError({
      status: 404,
      body: { error: 'Not Found', message: 'gone', code: 'missing' },
      requestUrl: 'https://api.example.com/api/subscriptions/x',
      requestMethod: 'GET',
      message: 'GET https://api.example.com/api/subscriptions/x failed (404): gone',
    });
    expect(err).toBeInstanceOf(StellarBillError);
    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe('StellarBillError');
    expect(err.status).toBe(404);
    expect(err.body?.code).toBe('missing');
    expect(err.requestUrl).toContain('/api/subscriptions/x');
    expect(err.requestMethod).toBe('GET');
  });

  it('toString includes method, url, status and body code', () => {
    const err = new StellarBillError({
      status: 400,
      body: { error: 'Bad Request', message: 'x', code: 'invalid_cursor' },
      requestUrl: 'https://api.example.com/api/v1/plans',
      requestMethod: 'GET',
      message: 'GET ... failed',
    });
    expect(err.toString()).toContain('StellarBillError');
    expect(err.toString()).toContain('GET');
    expect(err.toString()).toContain('invalid_cursor');
  });

  it('toString falls back to "unknown" when body lacks code', () => {
    const err = new StellarBillError({
      status: 500,
      body: undefined,
      requestUrl: 'https://api.example.com/api/health',
      requestMethod: 'GET',
      message: 'GET ... failed',
    });
    expect(err.toString()).toContain('(unknown)');
  });
});

describe('StellarBillConfigError', () => {
  it('is an Error subclass and instanceof works', () => {
    const err = new StellarBillConfigError('bad config');
    expect(err).toBeInstanceOf(StellarBillConfigError);
    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe('StellarBillConfigError');
    expect(err.message).toBe('bad config');
  });
});
