# S3 Statement Export

## Endpoint

```
POST /api/admin/statements/export
```

Requires a valid JWT with `manage:subscriptions` permission (admin or merchant role).

### Request body

```json
{
  "tenant_id":   "tenant-abc",
  "customer_id": "cust-xyz"
}
```

| Field         | Required | Description                                     |
|---------------|----------|-------------------------------------------------|
| `tenant_id`   | yes      | The tenant that owns the customer's statements  |
| `customer_id` | yes      | The customer whose statements to export         |

### Success response (200)

```json
{
  "object_key": "exports/tenant-abc/cust-xyz/20250623-153000.csv.gz",
  "url":        "https://my-bucket.s3.us-east-1.amazonaws.com/exports/...?X-Amz-Expires=900&...",
  "expires_at": "2025-06-23T15:45:00Z"
}
```

| Field        | Description                                              |
|--------------|----------------------------------------------------------|
| `object_key` | Versioned S3 key for the uploaded file                   |
| `url`        | Presigned GET URL, valid for 15 minutes                  |
| `expires_at` | UTC timestamp when the presigned URL expires (ISO 8601)  |

### Error responses

| HTTP | Condition                                                     |
|------|---------------------------------------------------------------|
| 400  | Missing or invalid `tenant_id` / `customer_id`               |
| 401  | Missing or invalid JWT                                        |
| 403  | Caller is not an admin, or merchant `caller_id ≠ tenant_id`  |
| 500  | S3 upload failed (after retries) or presign failed           |

---

## Object key schema

```
exports/{tenantID}/{customerID}/{YYYYMMDD-HHMMSS}.csv.gz
```

- **Tenant-scoped** — keys for different tenants never overlap.
- **Versioned by timestamp** — each export gets a unique key. Old exports remain in S3 but their presigned URLs expire after 15 minutes.

### Example key

```
exports/tenant-abc/cust-xyz/20250623-153000.csv.gz
```

---

## CSV format

The file is gzip-compressed. Decompress with `gunzip` or any gzip-aware tool.

```
id,subscription_id,customer_id,period_start,period_end,issued_at,total_amount,currency,kind,status
stmt-1,sub-1,cust-xyz,2025-01-01T00:00:00Z,2025-02-01T00:00:00Z,2025-02-02T00:00:00Z,2999,USD,invoice,paid
```

---

## Presigned URL TTL

Presigned URLs expire **15 minutes** after creation (`ExportPresignTTL`). This is enforced via the `X-Amz-Expires=900` query parameter embedded in the URL.

To change the TTL, update `service.ExportPresignTTL` in `internal/service/statement_service.go`.

---

## Revocation strategy

Because S3 presigned URLs are signed at generation time, they cannot be invalidated by rotating credentials alone. Two revocation paths are available:

1. **Object deletion (immediate)**: Delete the S3 object at the `object_key` returned in the response. Any download attempt against the presigned URL will return `404 NoSuchKey` immediately.

   ```bash
   aws s3 rm s3://<bucket>/exports/<tenantID>/<customerID>/<timestamp>.csv.gz
   ```

2. **TTL expiry (automatic)**: Do nothing — the URL stops working after 15 minutes.

For bulk revocation of all exports for a customer, delete the prefix:

```bash
aws s3 rm s3://<bucket>/exports/<tenantID>/<customerID>/ --recursive
```

---

## S3 configuration

Set these environment variables before starting the server:

| Variable              | Description                                         |
|-----------------------|-----------------------------------------------------|
| `S3_REGION`           | AWS region (e.g. `us-east-1`)                       |
| `S3_BUCKET`           | Bucket name                                         |
| `S3_ACCESS_KEY_ID`    | AWS access key ID                                   |
| `S3_SECRET_ACCESS_KEY`| AWS secret access key                               |
| `S3_ENDPOINT`         | Optional override (e.g. `http://localhost:4566` for LocalStack) |

---

## Retry / backoff

`PutObject` retries on HTTP 5xx responses with exponential backoff:

| Attempt | Backoff |
|---------|---------|
| 1 (initial) | 0 ms  |
| 2       | 100 ms  |
| 3       | 200 ms  |
| 4       | 400 ms  |

Default `MaxRetries = 3` (4 total attempts). Configurable via `s3.Config.MaxRetries`.
HTTP 4xx errors are **not** retried.

---

## Access control

| Caller role | Access                                                       |
|-------------|--------------------------------------------------------------|
| `admin`     | Always permitted, any `tenant_id`                           |
| `merchant`  | Permitted only when `caller_id == tenant_id`                |
| `subscriber`/ other | Always `403 Forbidden`                            |
