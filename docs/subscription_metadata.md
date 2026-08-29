# Subscription Metadata – API Contract

> **Contract:** `subscription_vault`  
> **Source:** `contracts/subscription_vault/src/metadata.rs`  
> **SDK:** `soroban-sdk 27.0.6`  
> **Storage type:** Persistent (survives indefinitely; archived rather than deleted when TTL expires)

---

## Overview

Each subscription in the vault can carry a bounded set of arbitrary key-value
string bytes.  These pairs are useful for attaching business data to a
subscription without modifying the core subscription record — for example,
payment provider references, discount tiers, CRM tags, or audit annotations.

Metadata is stored per-subscription in a `Map<Bytes, Bytes>` under persistent
storage keyed by `DataKey::Metadata(subscription_id)`.

---

## Limits

| Constraint | Value | Enforcement |
|---|---|---|
| Maximum keys per subscription | 10 (`MAX_METADATA_KEYS`) | Checked on insert of a _new_ key |
| Maximum key byte length | 32 (`MAX_METADATA_KEY_LENGTH`) | Byte length, not character count |
| Maximum value byte length | 256 (`MAX_METADATA_VALUE_LENGTH`) | Byte length, not character count |

> **Note:** Length checks use byte lengths, not character counts.  A 3-byte
> UTF-8 character counts as 3 bytes toward the limit.

---

## Functions

### `set_metadata`

```
pub fn set_metadata(
    env: Env,
    subscription_id: Bytes,
    caller: Address,
    key: Bytes,
    value: Bytes,
) -> Result<(), ContractError>
```

Creates or updates a metadata key for the given subscription.

**Behaviour**

- Loads the subscription record to resolve `subscriber` and `merchant`
  addresses.
- Verifies `caller == subscriber || caller == merchant`, then calls
  `caller.require_auth()`.
- Rejects keys longer than 32 bytes or values longer than 256 bytes.
- Rejects inserts that would push the key count above 10.  Updating an
  existing key at capacity is allowed.
- On success, stores the updated map and emits `MetadataSetEvent`.

**Error codes**

| Code | Meaning |
|---|---|
| `SubscriptionNotFound` (2) | `subscription_id` not registered |
| `Unauthorized` (1) | `caller` is neither subscriber nor merchant |
| `MetadataKeyTooLong` (10) | `key.len() > 32` |
| `MetadataValueTooLong` (11) | `value.len() > 256` |
| `MetadataKeyLimitReached` (12) | Adding a new key would exceed 10 |

---

### `get_metadata`

```
pub fn get_metadata(
    env: Env,
    subscription_id: Bytes,
    key: Bytes,
) -> Result<Bytes, ContractError>
```

Returns the value stored under `key` for the given subscription.

**Behaviour**

- No authorization required — any caller may read.
- Returns `MetadataKeyNotFound` if the key does not exist.
- Does not require the subscription to be registered; returns
  `MetadataKeyNotFound` for unknown subscriptions too (the map is empty).

**Error codes**

| Code | Meaning |
|---|---|
| `MetadataKeyNotFound` (13) | Key does not exist |

---

### `delete_metadata`

```
pub fn delete_metadata(
    env: Env,
    subscription_id: Bytes,
    caller: Address,
    key: Bytes,
) -> Result<(), ContractError>
```

Removes a key from the subscription's metadata map.

**Behaviour**

- Loads the subscription record to resolve `subscriber` and `merchant`.
- Verifies `caller == subscriber || caller == merchant`, then calls
  `caller.require_auth()`.
- Returns `MetadataKeyNotFound` if the key does not exist.
- On success, removes the key from storage and emits `MetadataDeletedEvent`.
- After deletion the freed slot can be used by a new key immediately.

**Error codes**

| Code | Meaning |
|---|---|
| `SubscriptionNotFound` (2) | `subscription_id` not registered |
| `Unauthorized` (1) | `caller` is neither subscriber nor merchant |
| `MetadataKeyNotFound` (13) | Key does not exist |

---

### `list_metadata_keys`

```
pub fn list_metadata_keys(
    env: Env,
    subscription_id: Bytes,
) -> Vec<Bytes>
```

Returns all keys currently stored for the subscription in an unordered
`Vec<Bytes>`.

**Behaviour**

- No authorization required.
- Returns an empty `Vec` if no metadata has been set, or if
  `subscription_id` is not registered.
- Does not return values; call `get_metadata` for each key if values
  are needed.

---

## Events

Events are defined using the `#[contractevent]` macro and are included in the
contract's ABI so that client SDKs can decode them without manual XDR parsing.

### `MetadataSetEvent`

Emitted by `set_metadata` on every successful write (create _or_ update).

| Field | Type | Description |
|---|---|---|
| `subscription_id` | `Bytes` | The subscription the key belongs to |
| `key` | `Bytes` | The key that was set or updated |
| `timestamp` | `u64` | Ledger timestamp at the time of write (seconds since Unix epoch) |

### `MetadataDeletedEvent`

Emitted by `delete_metadata` on every successful deletion.

| Field | Type | Description |
|---|---|---|
| `subscription_id` | `Bytes` | The subscription the key belonged to |
| `key` | `Bytes` | The key that was deleted |
| `timestamp` | `u64` | Ledger timestamp at the time of deletion |

---

## Authorization model

Two roles may mutate metadata for a subscription:

| Role | Address source |
|---|---|
| **Subscriber** | `Subscription.subscriber` field set at registration |
| **Merchant** | `Subscription.merchant` field set at registration |

Read operations (`get_metadata`, `list_metadata_keys`) are unrestricted.

The `caller` argument must equal one of these two addresses, or the call is
rejected with `ContractError::Unauthorized` _before_ `require_auth` is called.
This means an outsider's auth check never runs — the error is returned without
consuming any auth budget for the invalid caller.

---

## Storage design

```
DataKey::Metadata(subscription_id: Bytes)
  → Map<Bytes, Bytes>           (persistent storage)

DataKey::Subscription(subscription_id: Bytes)
  → Subscription { subscriber: Address, merchant: Address }  (persistent storage)
```

A single `Map<Bytes, Bytes>` per subscription is the unit of reads and writes.
The entire map is loaded and saved on each mutation, which is efficient for
maps up to the 10-key cap (well within Soroban's ledger-entry size limit for
persistent storage).

---

## Error code reference

```rust
pub enum ContractError {
    Unauthorized            = 1,
    SubscriptionNotFound    = 2,
    MetadataKeyTooLong      = 10,
    MetadataValueTooLong    = 11,
    MetadataKeyLimitReached = 12,
    MetadataKeyNotFound     = 13,
}
```

---

## Failure modes and safety properties

| Scenario | Outcome |
|---|---|
| Key at exact limit (32 bytes) | Accepted |
| Key one byte over limit (33 bytes) | `MetadataKeyTooLong` |
| Value at exact limit (256 bytes) | Accepted |
| Value one byte over limit (257 bytes) | `MetadataValueTooLong` |
| 10th unique key | Accepted |
| 11th unique key | `MetadataKeyLimitReached` |
| Updating an existing key when at capacity | Accepted (count does not increase) |
| Deleting a key then inserting a new key | Accepted (freed slot reused) |
| Outsider calling `set_metadata` | `Unauthorized` (before auth check) |
| `delete_metadata` on absent key | `MetadataKeyNotFound` |
| `get_metadata` on absent key | `MetadataKeyNotFound` |
| Operations on unregistered subscription | `SubscriptionNotFound` |
| `list_metadata_keys` on unregistered subscription | Empty `Vec` |

---

## Backward-compatibility notes

- The `list_metadata_keys` function deliberately returns an empty `Vec` (not an
  error) for unregistered subscriptions so that read-only callers do not need
  to handle `SubscriptionNotFound`.
- Error numeric codes are stable; adding new error variants in future must use
  previously-unused numbers.
- The `Map<Bytes, Bytes>` storage representation is stable.  Adding new fields
  to the per-subscription record in a future upgrade requires a storage
  migration (out-of-scope; to be documented separately).

---

## Test coverage

| Category | Tests |
|---|---|
| Happy paths | set/get by subscriber, set/get by merchant, update, delete-and-re-add, list after set/delete |
| Boundary validation | key 31/32/33 bytes, value 255/256/257 bytes, 10th key accepted, 11th rejected |
| Auth | outsider rejected for set and delete; merchant accepted for both |
| Subscription not found | set and delete on unregistered subscription |
| Missing key | get and delete on absent key; double-delete |
| Event emission | `MetadataSetEvent` and `MetadataDeletedEvent` verified via XDR |
| Isolation | two subscriptions with same key hold independent values |
| Capacity management | fill to limit, delete frees slot, duplicate key is update not insert |
| Concurrency simulation | sequential writes from multiple logical callers are consistent |
