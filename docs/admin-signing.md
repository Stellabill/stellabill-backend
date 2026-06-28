# Admin API Request Signing

This document describes how to sign requests to the Stellabill Admin API.

## Overview

All admin API requests must be signed with an HMAC-SHA256 signature to ensure authenticity and prevent tampering. The signature is computed over a canonical request string that includes the HTTP method, path, query parameters, selected headers, and a hash of the request body.

## Required Headers

Every admin API request must include the following headers:

1. `X-Stellabill-Date`: Unix timestamp in seconds (UTC) when the request was created. The timestamp must be within ±60 seconds of the server's current time.
2. `X-Stellabill-Request-ID`: A unique identifier for this request (e.g., a UUID). This is used to prevent replay attacks.
3. `X-Stellabill-Signature`: The HMAC-SHA256 signature of the canonical request, prefixed with `v1=`.

## Canonical Request Construction

The canonical request is a string constructed as follows:

```
<Method>
<Path>
<QueryString>
<SignedHeaders>
<SignedHeadersNames>
<BodyHash>
```

### Component Details

1. **Method**: The HTTP method in uppercase (e.g., `POST`, `GET`).
2. **Path**: The URL path (e.g., `/api/admin/purge`). If empty, use `/`.
3. **QueryString**: The sorted, URL-encoded query parameters, joined with `&`. Parameters are sorted lexicographically by key, then by value.
4. **SignedHeaders**: The signed headers in the format `key:value\n` for each header. The keys are lowercase and values are trimmed.
5. **SignedHeadersNames**: The sorted list of signed header names, joined with `;`.
6. **BodyHash**: The hex-encoded SHA-256 hash of the request body.

### Signed Headers

The following headers must be included in the canonical request:
- `x-stellabill-date`
- `x-stellabill-request-id`

## Signature Computation

To compute the signature:

1. Construct the canonical request string as described above.
2. Compute the HMAC-SHA256 of the canonical request using the admin signing secret key.
3. Encode the result as a hexadecimal string.
4. Prefix the hex string with `v1=`.

## Example

### Request

```http
POST /api/admin/purge?target=billing-cache&attempt=1 HTTP/1.1
Host: api.stellabill.com
X-Stellabill-Date: 1717356000
X-Stellabill-Request-ID: 550e8400-e29b-41d4-a716-446655440000
X-Stellabill-Signature: v1=...
Content-Type: application/json

{"partial": "0"}
```

### Canonical Request

```
POST
/api/admin/purge
attempt=1&target=billing-cache
x-stellabill-date:1717356000
x-stellabill-request-id:550e8400-e29b-41d4-a716-446655440000

x-stellabill-date;x-stellabill-request-id
44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a
```

### Signature Computation (Pseudocode)

```javascript
const secret = "your-admin-signing-secret";
const canonicalRequest = "..."; // as above

const hmac = crypto.createHmac("sha256", secret);
hmac.update(canonicalRequest);
const signature = "v1=" + hmac.digest("hex");
```

## Replay Protection

Each request ID (`X-Stellabill-Request-ID`) is cached for 5 minutes. Reusing a request ID within that time will result in a 401 Unauthorized response.
