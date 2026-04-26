# Strict JSON Decoding for Mutation Endpoints

## What and Why

Mutation endpoints (POST/PUT/PATCH) use a strict JSON decoder (`internal/decoder`) that:

- **Rejects unknown fields** — a typo like `"contarct_id"` returns `400 UNKNOWN_FIELD` instead of silently being ignored.
- **Enforces strict types** — sending `"sequence_num": "1"` (string instead of int) returns `400 INVALID_FIELD_TYPE`.
- **Rejects trailing data** — multiple JSON objects in one body return `400 INVALID_JSON`.

Read-only endpoints (GET) are unaffected; they have no request body.

## Error Codes

| Code | HTTP | Meaning |
|---|---|---|
| `UNKNOWN_FIELD` | 400 | Body contains a field not in the schema |
| `INVALID_FIELD_TYPE` | 400 | A field value has the wrong JSON type |
| `INVALID_JSON` | 400 | Malformed JSON, empty body, or trailing data |
| `INVALID_BODY` | 400 | Request body could not be read |

Error response shape:

```json
{
  "code": "UNKNOWN_FIELD",
  "message": "json: unknown field \"unexpected_field\""
}
```

## Backwards Compatibility Strategy

Strict decoding is **additive** for well-behaved clients:

- Clients that send only documented fields are unaffected.
- Clients that send extra fields (e.g. from a newer SDK talking to an older server) will receive a clear `400 UNKNOWN_FIELD` rather than silent data loss. This is intentional: it surfaces integration mismatches early.
- `null` values for optional fields are accepted (Go decodes them as zero values).
- Field ordering in the JSON object is irrelevant.

If a future API version needs to accept a new field, add it to the struct first, then deploy — no client breakage occurs.

## Applying to a New Endpoint

```go
import "stellarbill-backend/internal/decoder"

func MyMutationHandler(c *gin.Context) {
    var req MyRequest
    if err := decoder.DecodeStrict(c, &req); err != nil {
        return // response already written
    }
    // ... handle req
}
```

`DecodeStrict` writes the error response and returns a non-nil error. The caller only needs to `return`.

## Covered Endpoints

| Method | Path | Strict Decoding |
|---|---|---|
| POST | `/api/contract-events` | ✅ |

All future mutation endpoints should use `decoder.DecodeStrict`.
