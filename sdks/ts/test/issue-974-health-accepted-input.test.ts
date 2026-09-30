import { describe, expect, it } from 'vitest';
import { createStellarBillClient } from '../src/index.js';

describe('issue 974: accepted health response', () => {
  it('preserves the successful response envelope from getHealth', async () => {
    const fetch = async () => new Response(JSON.stringify({ status: 'ok' }), { status: 200, headers: { 'content-type': 'application/json' } });
    const result = await createStellarBillClient({ baseUrl: 'https://api.example.com', fetch }).getHealth();
    expect(result.status).toBe(200);
    expect(result.data).toEqual({ status: 'ok' });
    expect(result.error).toBeUndefined();
  });
});
