#![no_std]

/// Subscription Vault – Soroban smart contract.
///
/// Provides per-subscription key/value metadata storage with bounded capacity,
/// authorization enforcement, and event emission.
///
/// See `docs/subscription_metadata.md` for the full API contract.
use soroban_sdk::{contract, contracterror, contractimpl, contracttype, Address, Bytes, Env, Vec};

pub mod metadata;

#[cfg(test)]
mod test;

pub use metadata::{
    MetadataDeletedEvent, MetadataSetEvent, MAX_METADATA_KEY_LENGTH, MAX_METADATA_KEYS,
    MAX_METADATA_VALUE_LENGTH,
};

// ──────────────────────────────────────────────────────────────────────────────
// Storage keys
// ──────────────────────────────────────────────────────────────────────────────

/// All storage keys used by this contract.
///
/// `Metadata(subscription_id)` is the primary key for the per-subscription
/// metadata map stored in persistent storage.
#[contracttype]
#[derive(Clone, Debug, PartialEq)]
pub enum DataKey {
    /// Per-subscription metadata map: `Bytes → Bytes`.
    Metadata(Bytes),
    /// Subscription record (subscriber + merchant addresses).
    Subscription(Bytes),
}

// ──────────────────────────────────────────────────────────────────────────────
// Subscription record
// ──────────────────────────────────────────────────────────────────────────────

/// On-chain record that ties a subscription to its owner and the merchant.
///
/// This is stored in persistent storage when a subscription is registered.
/// The metadata functions use `subscriber` and `merchant` from this record for
/// auth decisions.
#[contracttype]
#[derive(Clone, Debug, PartialEq)]
pub struct Subscription {
    pub subscriber: Address,
    pub merchant: Address,
}

// ──────────────────────────────────────────────────────────────────────────────
// Error codes
// ──────────────────────────────────────────────────────────────────────────────

/// All error codes returned by this contract.
#[contracterror]
#[derive(Copy, Clone, Debug, PartialEq)]
pub enum ContractError {
    // ── Auth ──────────────────────────────────────────────────────────────────
    /// The caller is neither the subscriber nor the merchant.
    Unauthorized = 1,

    // ── Subscription ──────────────────────────────────────────────────────────
    /// The referenced subscription does not exist.
    SubscriptionNotFound = 2,

    // ── Metadata key/value validation ─────────────────────────────────────────
    /// The key exceeds `MAX_METADATA_KEY_LENGTH` bytes.
    MetadataKeyTooLong = 10,
    /// The value exceeds `MAX_METADATA_VALUE_LENGTH` bytes.
    MetadataValueTooLong = 11,
    /// Inserting a new key would exceed `MAX_METADATA_KEYS`.
    MetadataKeyLimitReached = 12,
    /// The key does not exist in this subscription's metadata.
    MetadataKeyNotFound = 13,
}

// ──────────────────────────────────────────────────────────────────────────────
// Contract
// ──────────────────────────────────────────────────────────────────────────────

#[contract]
pub struct SubscriptionVault;

#[contractimpl]
impl SubscriptionVault {
    // ── Subscription lifecycle ────────────────────────────────────────────────

    /// Register a new subscription.
    ///
    /// Both `subscriber` and `merchant` must authorize the registration.
    /// Calling this a second time with the same `subscription_id` overwrites the
    /// existing record (allowing ownership transfer with dual-sig consent).
    pub fn register_subscription(
        env: Env,
        subscription_id: Bytes,
        subscriber: Address,
        merchant: Address,
    ) -> Result<(), ContractError> {
        subscriber.require_auth();
        merchant.require_auth();

        let record = Subscription {
            subscriber,
            merchant,
        };
        let key = DataKey::Subscription(subscription_id);
        env.storage().persistent().set(&key, &record);
        Ok(())
    }

    // ── Metadata ──────────────────────────────────────────────────────────────

    /// Set or update a metadata key for a subscription.
    ///
    /// `caller` must be either the `subscriber` or the `merchant` registered for
    /// this subscription.  The subscription must have been previously registered
    /// via `register_subscription`.
    ///
    /// Enforces:
    /// - `key.len() <= MAX_METADATA_KEY_LENGTH`    (byte length)
    /// - `value.len() <= MAX_METADATA_VALUE_LENGTH` (byte length)
    /// - total keys per subscription `<= MAX_METADATA_KEYS`
    pub fn set_metadata(
        env: Env,
        subscription_id: Bytes,
        caller: Address,
        key: Bytes,
        value: Bytes,
    ) -> Result<(), ContractError> {
        let sub = Self::load_subscription(&env, &subscription_id)?;
        metadata::set_metadata(
            &env,
            &subscription_id,
            &caller,
            &sub.subscriber,
            &sub.merchant,
            key,
            value,
        )
    }

    /// Retrieve a single metadata value.
    ///
    /// Returns `ContractError::MetadataKeyNotFound` when the key is absent.
    /// No authorization required (read-only).
    pub fn get_metadata(
        env: Env,
        subscription_id: Bytes,
        key: Bytes,
    ) -> Result<Bytes, ContractError> {
        metadata::get_metadata(&env, &subscription_id, key)
    }

    /// Delete a metadata key.
    ///
    /// `caller` must be either the subscriber or the merchant.
    /// Returns `ContractError::MetadataKeyNotFound` when the key does not exist.
    pub fn delete_metadata(
        env: Env,
        subscription_id: Bytes,
        caller: Address,
        key: Bytes,
    ) -> Result<(), ContractError> {
        let sub = Self::load_subscription(&env, &subscription_id)?;
        metadata::delete_metadata(
            &env,
            &subscription_id,
            &caller,
            &sub.subscriber,
            &sub.merchant,
            key,
        )
    }

    /// List all metadata keys for a subscription.
    ///
    /// Returns an empty `Vec` when no metadata has been set.
    /// No authorization required.
    pub fn list_metadata_keys(env: Env, subscription_id: Bytes) -> Vec<Bytes> {
        metadata::list_metadata_keys(&env, &subscription_id)
    }

    // ── Internal helpers ──────────────────────────────────────────────────────

    fn load_subscription(env: &Env, subscription_id: &Bytes) -> Result<Subscription, ContractError> {
        let key = DataKey::Subscription(subscription_id.clone());
        env.storage()
            .persistent()
            .get(&key)
            .ok_or(ContractError::SubscriptionNotFound)
    }
}
