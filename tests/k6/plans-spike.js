import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const planErrors = new Counter('plan_errors');
const planLatency = new Trend('plan_latency');
const planP99 = new Trend('plan_p99_latency');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || 'test-key';
const PLANS_PATH = __ENV.PLANS_PATH || '/api/v1/plans';

export const options = {
  vus: 10,
  stages: [
    { duration: '5m', target: 2000 },
    { duration: '2m', target: 2000 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(99)<1500'],
    plan_p99_latency: ['p(99)<1500'],
    plan_errors: ['count<1'],
  },
};

export default function () {
  group('Plans Read - Spike', () => {
    const params = {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${API_KEY}`,
      },
    };

    const res = http.get(`${BASE_URL}${PLANS_PATH}`, params);

    const ok = check(res, {
      'status is 200': (r) => r.status === 200,
      'p99 < 1500ms': (r) => r.timings.duration < 1500,
      'body is not empty': (r) => r.body && r.body.length > 0,
    });

    if (!ok) planErrors.add(1);
    planLatency.add(res.timings.duration);
    planP99.add(res.timings.duration);

    sleep(1);
  });
}
