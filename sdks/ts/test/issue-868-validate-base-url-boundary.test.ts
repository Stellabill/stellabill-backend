import { describe, expect, it } from 'vitest';
import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

describe('issue 868: validateBaseUrl parse boundary', () => {
  it('distinguishes a non-empty malformed URL from an empty base URL', () => {
    const fetch = async () => new Response('{}', { status: 200 });
    expect(() => createStellarBillClient({ baseUrl: 'not a url', fetch })).toThrow(StellarBillConfigError);
    expect(() => createStellarBillClient({ baseUrl: 'not a url', fetch })).toThrow(/not a valid URL/);
    expect(() => createStellarBillClient({ baseUrl: '   ', fetch })).toThrow(/non-empty string/);
  });
});
