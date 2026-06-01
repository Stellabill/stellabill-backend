import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { authHeaders, loadtestTarget } from './utils.js';

export const errorRate = new Rate('errors');

export const options = {
  scenarios: {
    smoke: {
      executor: 'ramping-arrival-rate',
      startRate: 0,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 400,
      stages: [
        { duration: '20s', target: 50 },
        { duration: '25s', target: 150 },
        { duration: '65s', target: 200 },
        { duration: '20s', target: 200 },
        { duration: '10s', target: 0 },
      ],
    },
  },
  thresholds: {
    'http_req_duration{endpoint:plans}': ['p(95)<250'],
    errors: ['rate<0.001'],
  },
};

const target = loadtestTarget();
const headers = authHeaders();

export function setup() {
  const res = http.get(`${target}/api/v1/plans`, { headers, tags: { endpoint: 'plans', phase: 'warmup' } });
  const ok = check(res, {
    'warmup succeeded': (r) => r.status === 200,
  });
  if (!ok) {
    throw new Error(`warmup failed, expected 200 but got ${res.status}`);
  }
}

export default function () {
  const res = http.get(`${target}/api/v1/plans`, { headers, tags: { endpoint: 'plans' } });
  const success = check(res, {
    'status is 200': (r) => r.status === 200,
  });
  errorRate.add(!success);
  sleep(0.5);
}
