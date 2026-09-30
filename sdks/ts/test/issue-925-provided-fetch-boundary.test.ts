import { describe, expect, it } from 'vitest';
import { createStellarBillClient, StellarBillConfigError } from '../src/index.js';

describe('issue 925: providedFetch boundary', () => {
  it('rejects a non-callable explicit fetch instead of silently falling back', () => {
    expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: 0 as unknown as typeof fetch })).toThrow(StellarBillConfigError);
    expect(() => createStellarBillClient({ baseUrl: 'https://api.example.com', fetch: 0 as unknown as typeof fetch })).toThrow(/No fetch implementation available/);
  });
});
