import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Histogram, Trend } from 'k6/metrics';

// Custom metrics
const statementErrors = new Counter('statement_errors');
const responseTimes = new Histogram('statement_response_times');
const p95ResponseTime = new Trend('statement_p95_response_time');

// Configuration
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const DURATION = __ENV.DURATION || '1h';
const VUS = __ENV.VUS || 50;
const API_KEY = __ENV.API_KEY || 'test-key';

export const options = {
  stages: [
    { duration: '5m', target: VUS },      // Ramp up
    { duration: '50m', target: VUS },     // Sustain
    { duration: '5m', target: 0 },        // Ramp down
  ],
  thresholds: {
    'statement_response_times': ['p(95)<250', 'p(99)<500'],
    'http_req_status': ['count{status:200} > 0'],
    'statement_errors': ['count < 1'],
  },
};

export default function () {
  group('Statements List - Soak Test', () => {
    const params = {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${API_KEY}`,
      },
    };

    const res = http.get(`${BASE_URL}/api/v1/statements`, params);

    const isSuccess = check(res, {
      'status is 200': (r) => r.status === 200,
      'response time p95 < 250ms': (r) => r.timings.duration < 250,
      'body is not empty': (r) => r.body && r.body.length > 0,
    });

    if (!isSuccess) {
      statementErrors.add(1);
    }
    responseTimes.add(res.timings.duration);
    p95ResponseTime.add(res.timings.duration);

    sleep(Math.random() * 2 + 1);
  });
}
