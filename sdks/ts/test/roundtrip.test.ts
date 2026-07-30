import { afterEach, describe, expect, it, vi } from 'vitest';

import { assertOk, createStellarBillClient, SDK_VERSION } from '../src/index.js';
import type { components } from '../src/types/api.gen.js';

type HealthResp = components['schemas']['HealthResponse'];
type PlansResp = components['schemas']['PlansResponse'];
type SubResp = components['schemas']['SubscriptionsResponse'];
type Sub = components['schemas']['Subscription'];
type Idemp = components['schemas']['IdempotencyKeyRecord'];

function mockWithJson(json: unknown, status = 200): { fetch: typeof globalThis.fetch } {
  const res = new Response(JSON.stringify(json), { status, headers: { 'content-type': 'application/json' } });
  const fetch: typeof globalThis.fetch = vi.fn(async () => res);
  return { fetch };
}

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * End-to-end round-trip test: drives every documented operation through
 * the SDK and asserts the response shape matches the generated TypeScript
 * type. This guarantees the SDK's wrapper surface and the OpenAPI-
 * generated types stay in sync.
 */
describe('SDK round-trip (generated types match runtime responses)', () => {
  it('GET /api/health round-trips a typed HealthResponse', async () => {
    const sample: HealthResp = { status: 'ok', service: 'stellarbill-backend' };
    const { fetch } = mockWithJson(sample);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', token: 'rt-token', fetch });
    const data = await assertOk(await sdk.getHealth());
    // Compile-time assertion: the variable `typed` MUST match HealthResponse.
    const typed: HealthResp = data;
    expect(typed).toEqual(sample);
    expect(sdk.version).toBe(SDK_VERSION);
  });

  it('GET /api/v1/plans round-trips a typed PlansResponse', async () => {
    const sample: PlansResp = {
      plans: [
        { id: 'plan_basic', name: 'Basic', amount: '1000', currency: 'NGN', interval: 'monthly' },
      ],
      pagination: { has_more: false },
    };
    const { fetch } = mockWithJson(sample);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const data = await assertOk(await sdk.listPlans({ limit: 50 }));
    const typed: PlansResp = data;
    expect(typed.plans[0]!.id).toBe('plan_basic');
    expect(typed.pagination.has_more).toBe(false);
  });

  it('GET /api/subscriptions round-trips a typed SubscriptionsResponse', async () => {
    const sample: SubResp = {
      subscriptions: [
        {
          id: 'sub_123',
          plan_id: 'plan_basic',
          customer: 'cust_42',
          status: 'active',
          amount: '1500',
          interval: 'monthly',
          next_billing: '2026-12-31T00:00:00Z',
        },
      ],
      pagination: { has_more: true, next_cursor: 'Y3Vyc29yX25leHRfcGFnZQ==' },
    };
    const { fetch } = mockWithJson(sample);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const data = await assertOk(await sdk.listSubscriptions({ cursor: 'abc', limit: 25 }));
    const typed: SubResp = data;
    expect(typed.subscriptions[0]!.id).toBe('sub_123');
    expect(typed.pagination.has_more).toBe(true);
  });

  it('GET /api/subscriptions/{id} round-trips a typed Subscription', async () => {
    const sample: Sub = {
      id: 'sub_xyz',
      plan_id: 'plan_pro',
      customer: 'cust_xyz',
      status: 'active',
      amount: '5000',
      interval: 'monthly',
    };
    const { fetch } = mockWithJson(sample);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const data = await assertOk(await sdk.getSubscription('sub_xyz'));
    const typed: Sub = data;
    expect(typed.id).toBe('sub_xyz');
    expect(typed.plan_id).toBe('plan_pro');
  });

  it('GET /api/v1/idempotency/{key} round-trips a typed IdempotencyKeyRecord', async () => {
    const sample: Idemp = {
      key: 'order-abc-123',
      used_at: '2026-07-27T10:00:00Z',
      expires_at: '2026-07-28T10:00:00Z',
      status_code: 201,
      request_fingerprint: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    };
    const { fetch } = mockWithJson(sample);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const data = await assertOk(await sdk.inspectIdempotencyKey('order-abc-123'));
    const typed: Idemp = data;
    expect(typed.key).toBe('order-abc-123');
    expect(typed.status_code).toBe(201);
  });

  it('round-trip with a non-JSON error body returns undefined error data and typed envelope', async () => {
    const res = new Response('<html>500</html>', { status: 502, headers: { 'content-type': 'text/html' } });
    const fetch: typeof globalThis.fetch = vi.fn(async () => res);
    const sdk = createStellarBillClient({ baseUrl: 'https://api.example.com', fetch });
    const r = await sdk.getHealth();
    expect(r.status).toBe(502);
    expect(r.error).toBeUndefined();
    expect(r.data).toBeUndefined();
    expect(r.requestMethod).toBe('GET');
    expect(r.requestUrl).toContain('/api/health');
  });
});
