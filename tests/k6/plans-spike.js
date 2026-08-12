import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// Custom metrics
const planErrors = new Counter('plan_errors');
const planResponseTimes = new Trend('plan_response_times');

// Configuration (overridable via env)
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || 'test-key';

export const options = {
  // Ramp from 10 to 2000 VUs over 5 minutes, hold for 2 minutes,
  // then cool down. Focused on the plans read path (GET /api/v1/plans).
  stages: [
    { duration: '1m', target: 10 },      // warm-up to 10 VUs
    { duration: '5m', target: 2000 },    // ramp 10 -> 2000 (burst)
    { duration: '2m', target: 2000 },    // hold at peak
    { duration: '1m', target: 0 },       // cool down
  ],
  thresholds: {
    // p99 latency budget for the plans read path
    'plan_response_times': ['p(99)<500'],
    'http_req_duration': ['p(99)<500'],
    // Error rate must stay under 1%
    'http_req_failed': ['rate<0.01'],
    'plan_errors': ['count<1'],
    // At least 95% of checks must pass
    checks: ['rate>0.95'],
  },
};

export default function () {
  group('Plans Read Path - Spike Test', () => {
    const params = {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${API_KEY}`,
      },
    };

    const res = http.get(`${BASE_URL}/api/v1/plans`, params);

    const ok = check(res, {
      'status is 200': (r) => r.status === 200,
      'p99 < 500ms': (r) => r.timings.duration < 500,
      'body is not empty': (r) => r.body && r.body.length > 0,
    });

    if (!ok) {
      planErrors.add(1);
    }
    planResponseTimes.add(res.timings.duration);

    // Small think-time to approximate real client pacing.
    sleep(1);
  });
}
