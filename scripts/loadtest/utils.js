import encoding from 'k6/encoding';
import crypto from 'k6/crypto';

const DEFAULT_SECRET = 'dev-secret';
const DEFAULT_ROLE = 'merchant';
const DEFAULT_HOST = 'http://127.0.0.1:8080';
const DEFAULT_SUBJECT = 'loadtest-user';

export function loadtestTarget() {
  return __ENV.LOADTEST_TARGET || DEFAULT_HOST;
}

export function loadtestCustomerID() {
  return __ENV.LOADTEST_SUBJECT || DEFAULT_SUBJECT;
}

export function authHeaders() {
  if (__ENV.LOADTEST_JWT) {
    return {
      Authorization: `Bearer ${__ENV.LOADTEST_JWT}`,
      'Content-Type': 'application/json',
      'X-Tenant-ID': __ENV.LOADTEST_TENANT || 'loadtest-tenant',
    };
  }

  const secret = __ENV.JWT_SECRET || DEFAULT_SECRET;
  const token = createJwtToken(secret, __ENV.LOADTEST_ROLE || DEFAULT_ROLE, __ENV.LOADTEST_SUBJECT || DEFAULT_SUBJECT);

  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
    'X-Tenant-ID': __ENV.LOADTEST_TENANT || 'loadtest-tenant',
  };
}

function createJwtToken(secret, role, subject) {
  const header = { alg: 'HS256', typ: 'JWT' };
  const timestamp = Math.floor(Date.now() / 1000);
  const payload = {
    sub: subject,
    role,
    roles: [role],
    tenant: __ENV.LOADTEST_TENANT || 'loadtest-tenant',
    iat: timestamp,
    exp: timestamp + 3600,
  };

  const encodedHeader = base64UrlEncode(JSON.stringify(header));
  const encodedPayload = base64UrlEncode(JSON.stringify(payload));
  const signingInput = `${encodedHeader}.${encodedPayload}`;
  const signature = crypto
    .hmac('sha256', signingInput, secret, 'base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');

  return `${signingInput}.${signature}`;
}

function base64UrlEncode(value) {
  const encoded = encoding.b64encode(value);
  return encoded.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
