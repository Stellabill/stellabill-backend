# Subscription Metadata Privacy Constraints

This document defines what **must never** be stored in subscription or merchant
metadata fields, the allowlist of permitted fields, byte limits, encoding rules,
and the reviewer checklist for any change that touches metadata handling.

---

## Why this matters

Subscription metadata is attached to on-chain records in the Soroban contract
(`subscription_vault`). On-chain data is **public and permanent**. Any PII or
sensitive business data written there cannot be redacted after the fact.

---

## Do-not-store list (hard prohibition)

The following categories **must never** appear in any metadata field, key, or
value — whether on-chain or in the off-chain audit log:

| Category | Examples |
|---|---|
| Full name | `customer_name`, `full_name`, `first_name`, `last_name` |
| Contact details | Email address, phone number, postal address |
| Financial identifiers | Card number, bank account, IBAN, routing number |
| Government identifiers | National ID, passport number, tax ID, SSN |
| Authentication secrets | Passwords, API keys, JWT tokens, OAuth tokens |
| Biometric data | Fingerprint hash, face ID reference |
| Health / medical data | Any field that could identify a medical condition |
| Free-form notes | Unstructured text fields that may accidentally contain PII |

Validation **must reject** any metadata entry whose key or value matches a
pattern from this list (see [Validation rules](#validation-rules) below).

---

## Allowlist of permitted metadata fields

Only the following keys are accepted. Any key not on this list is rejected with
a validation error.

| Key | Type | Max bytes | Description |
|---|---|---|---|
| `plan_tier` | string | 32 | Internal tier label (e.g. `"starter"`, `"pro"`) |
| `billing_cycle` | string | 16 | `"monthly"` or `"annual"` |
| `promo_code` | string | 24 | Opaque promotional code (no PII) |
| `region_code` | string | 8 | ISO 3166-1 alpha-2 country code |
| `source_channel` | string | 32 | Acquisition channel label (e.g. `"organic"`) |
| `trial_days` | string | 4 | Number of trial days as a decimal string |
| `currency` | string | 3 | ISO 4217 currency code |

Total metadata payload: **≤ 512 bytes** (UTF-8 encoded, all keys + values
combined).

---

## Encoding rules

1. All keys and values **must** be valid UTF-8.
2. Keys **must** match `^[a-z][a-z0-9_]{0,31}$` (lowercase, no spaces).
3. Values **must not** contain control characters (`\x00`–`\x1f`, `\x7f`).
4. The total serialised size of the metadata map **must not** exceed 512 bytes.
5. A maximum of **7 key-value pairs** is allowed per metadata object.

---

## Validation rules

Validation is enforced at two layers:

### 1. Off-chain (Go backend — `internal/audit/sink.go`)

The audit logger already redacts values that look like bearer tokens or
passwords (see `redact()` in `internal/audit/logger.go`). Metadata submitted
to any API endpoint is additionally validated against:

- Key allowlist check — reject unknown keys.
- Value size check — reject values exceeding per-field byte limits.
- Total payload size check — reject if combined size > 512 bytes.
- Control-character scan — reject values containing `\x00`–`\x1f`.

### 2. On-chain (Rust — `metadata.rs` in `subscription_vault`)

The Soroban contract enforces:

- Maximum 7 entries per map.
- Each key ≤ 32 bytes, each value ≤ 64 bytes.
- Total payload ≤ 512 bytes.
- Rejects any key not in the contract-level allowlist symbol set.

---

## Reviewer checklist

When reviewing any PR that touches metadata handling, confirm:

- [ ] No new key accepts free-form user input without explicit sanitisation.
- [ ] No key name or value pattern could encode PII (even indirectly).
- [ ] The allowlist in this document and in `metadata.rs` are kept in sync.
- [ ] New keys are added to the allowlist table above **before** merging.
- [ ] Validation tests cover: max-size payload, unknown key, control character,
      and a value that looks like a bearer token.
- [ ] The PR description includes a short privacy note explaining why the new
      field cannot contain PII.

---

## Test coverage requirements

Tests for metadata validation must cover:

| Scenario | Expected result |
|---|---|
| Valid payload within limits | Accepted |
| Unknown key | Rejected with descriptive error |
| Value exceeds per-field byte limit | Rejected |
| Total payload > 512 bytes | Rejected |
| Value contains control character (`\x00`) | Rejected |
| Value looks like a bearer token | Rejected / redacted |
| More than 7 key-value pairs | Rejected |
| Non-UTF-8 bytes in value | Rejected |

Minimum coverage for metadata validation code: **95 %**.

---

## Related files

- `internal/audit/logger.go` — off-chain redaction logic
- `internal/audit/sink.go` — sink durability and failure policy
- `openapi/openapi.yaml` — API schema (metadata field descriptions)
- `contracts/subscription_vault/src/metadata.rs` — on-chain enforcement
- `docs/security-notes.md` — broader security considerations

---

## Change history

| Date | Author | Summary |
|---|---|---|
| 2026-04-24 | icodeBisola | Initial privacy constraints document (closes stellabill-contracts#288) |
