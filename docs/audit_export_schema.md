# Audit Log Export Schema

This document outlines the schema of the nightly audit log export that is sent to the data warehouse for retention and long-term analytics.

## Export Format

The export is provided as a JSON Lines (JSONL) file, where each line is a valid JSON object representing a single audit event. 

## Schema Definition

For `feature_flag_toggle` events, the exported record contains the following fields:

| Field | Type | Description |
| :--- | :--- | :--- |
| `timestamp` | `string` (RFC3339) | The exact time the audit event occurred. |
| `actor` | `string` | The identifier (e.g., username, IP, or service identity) of the user who performed the action. |
| `action` | `string` | The action performed. For feature flags, this will be `feature_flag_toggle`. |
| `resource` | `string` | The target resource of the action. For feature flags, this is the name of the feature flag. |
| `outcome` | `string` | The result of the action (e.g., `success`). |
| `reason` | `string` | The human-readable reason provided by the actor for why the feature flag was toggled. |
| `before_enable` | `string` (`"true"`/`"false"`) | The state of the feature flag before the toggle action. |
| `after_enable` | `string` (`"true"`/`"false"`) | The state of the feature flag after the toggle action. |
| `hash` | `string` | Cryptographic HMAC-SHA256 hash ensuring the integrity and non-repudiation of the audit log entry. |

### Example Record

```json
{
  "timestamp": "2026-07-28T16:12:48Z",
  "actor": "admin@example.com",
  "action": "feature_flag_toggle",
  "resource": "enable_new_billing_engine",
  "outcome": "success",
  "reason": "Gradual rollout for beta testers",
  "before_enable": "false",
  "after_enable": "true",
  "hash": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a7b8c9d0e1f2"
}
```

## Security and Compliance Notes

- **Data Redaction:** Sensitive information such as passwords, API keys, card details, or authentication tokens are automatically redacted by the internal audit logger (`[REDACTED]`) before the audit event is persisted or exported. No sensitive values are exported.
- **Integrity Verification:** The `hash` field represents a cryptographic chain with the preceding log event. The data warehouse can verify the sequence of hashes to detect tampering or missing logs.
