import { describe, expect, it } from 'vitest';
import { createStellarBillClient } from '../src/index.js';

describe('issue 853: getToken boundary', () => {
  it('returns undefined before configuration and the exact token after rotation', () => {
    const fetch = async () => new Response('{}', { status: 200 });
    const client = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    expect(client.getToken()).toBeUndefined();
    client.setToken('boundary-token');
    expect(client.getToken()).toBe('boundary-token');
    client.setToken(undefined);
    expect(client.getToken()).toBeUndefined();
  });
});
