/// Bounded persistent metadata storage for subscription vault.
///
/// Each subscription can hold at most `MAX_METADATA_KEYS` key-value pairs.
/// Keys must be at most `MAX_METADATA_KEY_LENGTH` bytes.
/// Values must be at most `MAX_METADATA_VALUE_LENGTH` bytes.
/// Byte lengths (not character counts) are enforced to match Soroban semantics.
///
/// Auth: only the subscription owner (subscriber) or the merchant may write or
/// delete metadata.  Read operations are unrestricted.
///
/// Events emitted:
///   - `MetadataSetEvent`     on every successful write
///   - `MetadataDeletedEvent` on every successful delete
use soroban_sdk::{contractevent, Address, Bytes, Env, Map, Vec};

use crate::{ContractError, DataKey};

// ──────────────────────────────────────────────────────────────────────────────
// Limits (all measured in bytes)
// ──────────────────────────────────────────────────────────────────────────────

/// Maximum number of metadata keys per subscription.
pub const MAX_METADATA_KEYS: u32 = 10;

/// Maximum byte length of a metadata key.
pub const MAX_METADATA_KEY_LENGTH: u32 = 32;

/// Maximum byte length of a metadata value.
pub const MAX_METADATA_VALUE_LENGTH: u32 = 256;

// ──────────────────────────────────────────────────────────────────────────────
// Event payload types
// ──────────────────────────────────────────────────────────────────────────────

/// Emitted when a metadata key is created or updated.
///
/// The `#[contractevent]` macro adds a `.publish(&env)` method and exports the
/// event type into the contract's ABI so that clients and tooling can decode it.
#[contractevent]
pub struct MetadataSetEvent {
    /// The subscription this metadata belongs to.
    pub subscription_id: Bytes,
    /// The key that was set or updated.
    pub key: Bytes,
    /// Ledger timestamp (seconds since Unix epoch) when the write occurred.
    pub timestamp: u64,
}

/// Emitted when a metadata key is deleted.
#[contractevent]
pub struct MetadataDeletedEvent {
    /// The subscription this metadata belongs to.
    pub subscription_id: Bytes,
    /// The key that was deleted.
    pub key: Bytes,
    /// Ledger timestamp (seconds since Unix epoch) when the delete occurred.
    pub timestamp: u64,
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

/// Loads the metadata map for `subscription_id`, or returns an empty map.
fn load_map(env: &Env, subscription_id: &Bytes) -> Map<Bytes, Bytes> {
    let key = DataKey::Metadata(subscription_id.clone());
    env.storage()
        .persistent()
        .get(&key)
        .unwrap_or_else(|| Map::new(env))
}

/// Persists `map` back to storage.
fn save_map(env: &Env, subscription_id: &Bytes, map: &Map<Bytes, Bytes>) {
    let key = DataKey::Metadata(subscription_id.clone());
    env.storage().persistent().set(&key, map);
}

/// Returns the current ledger timestamp (seconds since Unix epoch).
fn now(env: &Env) -> u64 {
    env.ledger().timestamp()
}

// ──────────────────────────────────────────────────────────────────────────────
// Public API
// ──────────────────────────────────────────────────────────────────────────────

/// Set or update a metadata key for a subscription.
///
/// # Auth
/// `caller` must be either the `subscriber` or the `merchant` recorded for
/// this subscription.  Both addresses are passed by the contract dispatcher
/// (verified against on-chain subscription state); this function calls
/// `require_auth` on `caller`.
///
/// # Errors
/// - `ContractError::Unauthorized`         – if `caller` is neither subscriber nor merchant
/// - `ContractError::MetadataKeyTooLong`   – if `key.len() > MAX_METADATA_KEY_LENGTH`
/// - `ContractError::MetadataValueTooLong` – if `value.len() > MAX_METADATA_VALUE_LENGTH`
/// - `ContractError::MetadataKeyLimitReached` – if inserting a new key would exceed the cap
pub fn set_metadata(
    env: &Env,
    subscription_id: &Bytes,
    caller: &Address,
    subscriber: &Address,
    merchant: &Address,
    key: Bytes,
    value: Bytes,
) -> Result<(), ContractError> {
    // ── Auth ─────────────────────────────────────────────────────────────────
    if caller != subscriber && caller != merchant {
        return Err(ContractError::Unauthorized);
    }
    caller.require_auth();

    // ── Validate lengths ──────────────────────────────────────────────────────
    if key.len() > MAX_METADATA_KEY_LENGTH {
        return Err(ContractError::MetadataKeyTooLong);
    }
    if value.len() > MAX_METADATA_VALUE_LENGTH {
        return Err(ContractError::MetadataValueTooLong);
    }

    // ── Capacity check ───────────────────────────────────────────────────────
    let mut map = load_map(env, subscription_id);
    let key_exists = map.contains_key(key.clone());
    if !key_exists && map.len() >= MAX_METADATA_KEYS {
        return Err(ContractError::MetadataKeyLimitReached);
    }

    // ── Persist ──────────────────────────────────────────────────────────────
    map.set(key.clone(), value);
    save_map(env, subscription_id, &map);

    // ── Emit event ───────────────────────────────────────────────────────────
    MetadataSetEvent {
        subscription_id: subscription_id.clone(),
        key,
        timestamp: now(env),
    }
    .publish(env);

    Ok(())
}

/// Retrieve a single metadata value by key.
///
/// Returns `Err(ContractError::MetadataKeyNotFound)` if the key does not exist.
/// This call is unrestricted — any caller may read metadata.
pub fn get_metadata(
    env: &Env,
    subscription_id: &Bytes,
    key: Bytes,
) -> Result<Bytes, ContractError> {
    let map = load_map(env, subscription_id);
    map.get(key).ok_or(ContractError::MetadataKeyNotFound)
}

/// Delete a metadata key.
///
/// # Auth
/// Same as `set_metadata`: `caller` must be subscriber or merchant.
///
/// # Errors
/// - `ContractError::Unauthorized`        – auth failure
/// - `ContractError::MetadataKeyNotFound` – key does not exist
pub fn delete_metadata(
    env: &Env,
    subscription_id: &Bytes,
    caller: &Address,
    subscriber: &Address,
    merchant: &Address,
    key: Bytes,
) -> Result<(), ContractError> {
    // ── Auth ─────────────────────────────────────────────────────────────────
    if caller != subscriber && caller != merchant {
        return Err(ContractError::Unauthorized);
    }
    caller.require_auth();

    // ── Check key exists ─────────────────────────────────────────────────────
    let mut map = load_map(env, subscription_id);
    if !map.contains_key(key.clone()) {
        return Err(ContractError::MetadataKeyNotFound);
    }

    // ── Persist ──────────────────────────────────────────────────────────────
    map.remove(key.clone());
    save_map(env, subscription_id, &map);

    // ── Emit event ───────────────────────────────────────────────────────────
    MetadataDeletedEvent {
        subscription_id: subscription_id.clone(),
        key,
        timestamp: now(env),
    }
    .publish(env);

    Ok(())
}

/// Return all metadata keys for a subscription as a `Vec<Bytes>`.
///
/// Returns an empty `Vec` when no metadata exists.
/// This call is unrestricted.
pub fn list_metadata_keys(env: &Env, subscription_id: &Bytes) -> Vec<Bytes> {
    let map = load_map(env, subscription_id);
    map.keys()
}
