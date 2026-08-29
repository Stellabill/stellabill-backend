#![cfg(test)]

extern crate std;
use std::format;
use std::vec::Vec as StdVec;

use soroban_sdk::{
    testutils::{Address as _, Events as _},
    Address, Bytes, Env, Event,
};

use crate::{
    metadata::{MetadataDeletedEvent, MetadataSetEvent, MAX_METADATA_KEYS},
    ContractError, SubscriptionVault,
};

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

struct Setup {
    env: Env,
    contract_id: Address,
    sub_id: Bytes,
    subscriber: Address,
    merchant: Address,
}

impl Setup {
    fn new() -> Self {
        let env = Env::default();
        env.mock_all_auths();

        let contract_id = env.register(SubscriptionVault, ());
        let sub_id = Bytes::from_slice(&env, b"sub-0001");
        let subscriber = Address::generate(&env);
        let merchant = Address::generate(&env);

        env.as_contract(&contract_id, || {
            crate::SubscriptionVault::register_subscription(
                env.clone(),
                sub_id.clone(),
                subscriber.clone(),
                merchant.clone(),
            )
            .unwrap();
        });

        Setup {
            env,
            contract_id,
            sub_id,
            subscriber,
            merchant,
        }
    }

    fn set(&self, key: &[u8], value: &[u8]) -> Result<(), ContractError> {
        let env = &self.env;
        let key = Bytes::from_slice(env, key);
        let value = Bytes::from_slice(env, value);
        env.as_contract(&self.contract_id, || {
            SubscriptionVault::set_metadata(
                env.clone(),
                self.sub_id.clone(),
                self.subscriber.clone(),
                key,
                value,
            )
        })
    }

    fn set_by(&self, caller: &Address, key: &[u8], value: &[u8]) -> Result<(), ContractError> {
        let env = &self.env;
        let key = Bytes::from_slice(env, key);
        let value = Bytes::from_slice(env, value);
        env.as_contract(&self.contract_id, || {
            SubscriptionVault::set_metadata(
                env.clone(),
                self.sub_id.clone(),
                caller.clone(),
                key,
                value,
            )
        })
    }

    fn get(&self, key: &[u8]) -> Result<Bytes, ContractError> {
        let env = &self.env;
        let key = Bytes::from_slice(env, key);
        env.as_contract(&self.contract_id, || {
            SubscriptionVault::get_metadata(env.clone(), self.sub_id.clone(), key)
        })
    }

    fn delete(&self, caller: &Address, key: &[u8]) -> Result<(), ContractError> {
        let env = &self.env;
        let key = Bytes::from_slice(env, key);
        env.as_contract(&self.contract_id, || {
            SubscriptionVault::delete_metadata(
                env.clone(),
                self.sub_id.clone(),
                caller.clone(),
                key,
            )
        })
    }

    fn list(&self) -> soroban_sdk::Vec<Bytes> {
        let env = &self.env;
        env.as_contract(&self.contract_id, || {
            SubscriptionVault::list_metadata_keys(env.clone(), self.sub_id.clone())
        })
    }

    fn bytes(&self, s: &[u8]) -> Bytes {
        Bytes::from_slice(&self.env, s)
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Happy paths
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_set_and_get_metadata_by_subscriber() {
    let s = Setup::new();
    s.set(b"plan_tier", b"gold").unwrap();
    assert_eq!(s.get(b"plan_tier").unwrap(), s.bytes(b"gold"));
}

#[test]
fn test_set_and_get_metadata_by_merchant() {
    let s = Setup::new();
    s.set_by(&s.merchant.clone(), b"label", b"vip").unwrap();
    assert_eq!(s.get(b"label").unwrap(), s.bytes(b"vip"));
}

#[test]
fn test_update_existing_key() {
    let s = Setup::new();
    s.set(b"status", b"active").unwrap();
    s.set(b"status", b"paused").unwrap();
    assert_eq!(s.get(b"status").unwrap(), s.bytes(b"paused"));
}

#[test]
fn test_delete_and_re_add_key() {
    let s = Setup::new();
    s.set(b"note", b"first").unwrap();
    s.delete(&s.subscriber.clone(), b"note").unwrap();
    s.set(b"note", b"second").unwrap();
    assert_eq!(s.get(b"note").unwrap(), s.bytes(b"second"));
}

#[test]
fn test_list_metadata_keys_empty() {
    let s = Setup::new();
    assert_eq!(s.list().len(), 0);
}

#[test]
fn test_list_metadata_keys_after_sets() {
    let s = Setup::new();
    s.set(b"k1", b"v1").unwrap();
    s.set(b"k2", b"v2").unwrap();
    assert_eq!(s.list().len(), 2);
}

#[test]
fn test_list_metadata_keys_after_delete() {
    let s = Setup::new();
    s.set(b"k1", b"v1").unwrap();
    s.set(b"k2", b"v2").unwrap();
    s.delete(&s.subscriber.clone(), b"k1").unwrap();
    let keys = s.list();
    assert_eq!(keys.len(), 1);
    assert_eq!(keys.get(0).unwrap(), s.bytes(b"k2"));
}

#[test]
fn test_up_to_max_keys() {
    let s = Setup::new();
    for i in 0u32..MAX_METADATA_KEYS {
        let k = format!("key_{:03}", i);
        s.set(k.as_bytes(), b"v").unwrap();
    }
    assert_eq!(s.list().len(), MAX_METADATA_KEYS);
}

// ──────────────────────────────────────────────────────────────────────────────
// Boundary validation
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_exact_max_key_length_accepted() {
    let s = Setup::new();
    let key = Bytes::from_array(&s.env, &[b'x'; 32]);
    let val = s.bytes(b"val");
    let result = s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            val,
        )
    });
    assert!(result.is_ok(), "expected Ok, got {:?}", result);
}

#[test]
fn test_key_33_bytes_rejected() {
    let s = Setup::new();
    let key = Bytes::from_array(&s.env, &[b'x'; 33]);
    let val = s.bytes(b"val");
    let err = s
        .env
        .as_contract(&s.contract_id, || {
            SubscriptionVault::set_metadata(
                s.env.clone(),
                s.sub_id.clone(),
                s.subscriber.clone(),
                key,
                val,
            )
        })
        .unwrap_err();
    assert_eq!(err, ContractError::MetadataKeyTooLong);
}

#[test]
fn test_exact_max_value_length_accepted() {
    let s = Setup::new();
    let value = Bytes::from_array(&s.env, &[b'v'; 256]);
    let key = s.bytes(b"k");
    let result = s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            value,
        )
    });
    assert!(result.is_ok(), "expected Ok, got {:?}", result);
}

#[test]
fn test_value_257_bytes_rejected() {
    let s = Setup::new();
    let value = Bytes::from_array(&s.env, &[b'v'; 257]);
    let key = s.bytes(b"k");
    let err = s
        .env
        .as_contract(&s.contract_id, || {
            SubscriptionVault::set_metadata(
                s.env.clone(),
                s.sub_id.clone(),
                s.subscriber.clone(),
                key,
                value,
            )
        })
        .unwrap_err();
    assert_eq!(err, ContractError::MetadataValueTooLong);
}

#[test]
fn test_eleventh_key_rejected() {
    let s = Setup::new();
    // Fill to limit
    for i in 0u32..MAX_METADATA_KEYS {
        let k = format!("key_{:03}", i);
        s.set(k.as_bytes(), b"v").unwrap();
    }
    // One more distinct key must fail
    let err = s.set(b"overflow_key", b"v").unwrap_err();
    assert_eq!(err, ContractError::MetadataKeyLimitReached);
}

#[test]
fn test_update_does_not_count_toward_limit() {
    let s = Setup::new();
    // Fill to limit
    for i in 0u32..MAX_METADATA_KEYS {
        let k = format!("key_{:03}", i);
        s.set(k.as_bytes(), b"v").unwrap();
    }
    // Updating an existing key is allowed even at capacity
    let result = s.set(b"key_000", b"new_value");
    assert!(result.is_ok(), "expected Ok, got {:?}", result);
}

#[test]
fn test_empty_key_accepted() {
    let s = Setup::new();
    assert!(s.set(b"", b"value").is_ok());
}

#[test]
fn test_empty_value_accepted() {
    let s = Setup::new();
    assert!(s.set(b"key", b"").is_ok());
}

// ──────────────────────────────────────────────────────────────────────────────
// Authorization boundaries
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_non_owner_cannot_set_metadata() {
    let s = Setup::new();
    let outsider = Address::generate(&s.env);
    let err = s.set_by(&outsider, b"k", b"v").unwrap_err();
    assert_eq!(err, ContractError::Unauthorized);
}

#[test]
fn test_non_owner_cannot_delete_metadata() {
    let s = Setup::new();
    s.set(b"k", b"v").unwrap();
    let outsider = Address::generate(&s.env);
    let err = s.delete(&outsider, b"k").unwrap_err();
    assert_eq!(err, ContractError::Unauthorized);
}

#[test]
fn test_merchant_can_set_metadata() {
    let s = Setup::new();
    let m = s.merchant.clone();
    assert!(s.set_by(&m, b"merchant_key", b"merchant_value").is_ok());
}

#[test]
fn test_merchant_can_delete_metadata() {
    let s = Setup::new();
    s.set(b"to_delete", b"v").unwrap();
    let m = s.merchant.clone();
    assert!(s.delete(&m, b"to_delete").is_ok());
}

// ──────────────────────────────────────────────────────────────────────────────
// Subscription not found
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_set_metadata_on_nonexistent_subscription() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(SubscriptionVault, ());
    let caller = Address::generate(&env);
    let bad_id = Bytes::from_slice(&env, b"does-not-exist");

    let err = env
        .as_contract(&contract_id, || {
            SubscriptionVault::set_metadata(
                env.clone(),
                bad_id,
                caller,
                Bytes::from_slice(&env, b"k"),
                Bytes::from_slice(&env, b"v"),
            )
        })
        .unwrap_err();
    assert_eq!(err, ContractError::SubscriptionNotFound);
}

#[test]
fn test_delete_metadata_on_nonexistent_subscription() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(SubscriptionVault, ());
    let caller = Address::generate(&env);
    let bad_id = Bytes::from_slice(&env, b"does-not-exist");

    let err = env
        .as_contract(&contract_id, || {
            SubscriptionVault::delete_metadata(
                env.clone(),
                bad_id,
                caller,
                Bytes::from_slice(&env, b"k"),
            )
        })
        .unwrap_err();
    assert_eq!(err, ContractError::SubscriptionNotFound);
}

// ──────────────────────────────────────────────────────────────────────────────
// Get / Delete missing key
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_get_nonexistent_key() {
    let s = Setup::new();
    let err = s.get(b"ghost").unwrap_err();
    assert_eq!(err, ContractError::MetadataKeyNotFound);
}

#[test]
fn test_delete_nonexistent_key() {
    let s = Setup::new();
    let sub = s.subscriber.clone();
    let err = s.delete(&sub, b"ghost").unwrap_err();
    assert_eq!(err, ContractError::MetadataKeyNotFound);
}

// ──────────────────────────────────────────────────────────────────────────────
// Event emission
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_set_metadata_emits_event() {
    let s = Setup::new();
    let key = s.bytes(b"event_key");
    let value = s.bytes(b"event_value");

    // The expected event for comparison
    let expected_event = MetadataSetEvent {
        subscription_id: s.sub_id.clone(),
        key: key.clone(),
        timestamp: s.env.ledger().timestamp(),
    };

    s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            value,
        )
        .unwrap();
    });

    // Verify via XDR comparison (recommended approach in sdk 27+)
    let all_events = s.env.events().all();
    assert!(
        !all_events.events().is_empty(),
        "no events emitted after set_metadata"
    );

    let expected_xdr = expected_event.to_xdr(&s.env, &s.contract_id);
    assert!(
        all_events
            .events()
            .iter()
            .any(|e| e == &expected_xdr),
        "MetadataSetEvent not found in emitted events"
    );
}

#[test]
fn test_delete_metadata_emits_event() {
    let s = Setup::new();
    let key = s.bytes(b"del_key");

    s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key.clone(),
            s.bytes(b"v"),
        )
        .unwrap();
    });

    let expected_event = MetadataDeletedEvent {
        subscription_id: s.sub_id.clone(),
        key: key.clone(),
        timestamp: s.env.ledger().timestamp(),
    };

    s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::delete_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
        )
        .unwrap();
    });

    let all_events = s.env.events().all();
    let expected_xdr = expected_event.to_xdr(&s.env, &s.contract_id);
    assert!(
        all_events
            .events()
            .iter()
            .any(|e| e == &expected_xdr),
        "MetadataDeletedEvent not found in emitted events"
    );
}

// ──────────────────────────────────────────────────────────────────────────────
// Multiple subscriptions are isolated
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_separate_subscriptions_are_isolated() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(SubscriptionVault, ());

    let sub_a = Bytes::from_slice(&env, b"sub-A");
    let sub_b = Bytes::from_slice(&env, b"sub-B");
    let subscriber_a = Address::generate(&env);
    let merchant_a = Address::generate(&env);
    let subscriber_b = Address::generate(&env);
    let merchant_b = Address::generate(&env);
    let key = Bytes::from_slice(&env, b"shared_key");

    // Register both subscriptions
    env.as_contract(&contract_id, || {
        SubscriptionVault::register_subscription(
            env.clone(),
            sub_a.clone(),
            subscriber_a.clone(),
            merchant_a.clone(),
        )
        .unwrap();
    });
    env.as_contract(&contract_id, || {
        SubscriptionVault::register_subscription(
            env.clone(),
            sub_b.clone(),
            subscriber_b.clone(),
            merchant_b.clone(),
        )
        .unwrap();
    });

    // Write different values for the same key on each subscription
    env.as_contract(&contract_id, || {
        SubscriptionVault::set_metadata(
            env.clone(),
            sub_a.clone(),
            subscriber_a.clone(),
            key.clone(),
            Bytes::from_slice(&env, b"value_a"),
        )
        .unwrap();
    });
    env.as_contract(&contract_id, || {
        SubscriptionVault::set_metadata(
            env.clone(),
            sub_b.clone(),
            subscriber_b.clone(),
            key.clone(),
            Bytes::from_slice(&env, b"value_b"),
        )
        .unwrap();
    });

    // Verify each subscription sees its own value
    let got_a = env.as_contract(&contract_id, || {
        SubscriptionVault::get_metadata(env.clone(), sub_a.clone(), key.clone()).unwrap()
    });
    let got_b = env.as_contract(&contract_id, || {
        SubscriptionVault::get_metadata(env.clone(), sub_b.clone(), key.clone()).unwrap()
    });

    assert_eq!(got_a, Bytes::from_slice(&env, b"value_a"));
    assert_eq!(got_b, Bytes::from_slice(&env, b"value_b"));
}

// ──────────────────────────────────────────────────────────────────────────────
// Delete-then-refill confirms freed slot is reusable
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_delete_frees_slot_for_new_key() {
    let s = Setup::new();

    // Fill to capacity
    for i in 0u32..MAX_METADATA_KEYS {
        let k = format!("key_{:03}", i);
        s.set(k.as_bytes(), b"v").unwrap();
    }

    // Delete one key
    let sub = s.subscriber.clone();
    s.delete(&sub, b"key_000").unwrap();

    // Now inserting a brand new key should succeed
    let result = s.set(b"brand_new", b"v");
    assert!(
        result.is_ok(),
        "expected Ok after freeing a slot, got {:?}",
        result
    );
}

// ──────────────────────────────────────────────────────────────────────────────
// Duplicate key insert (update) does not inflate the count
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_duplicate_key_insert_is_update() {
    let s = Setup::new();

    for _ in 0..5 {
        s.set(b"dup", b"val").unwrap();
    }

    // Exactly 1 key stored
    assert_eq!(s.list().len(), 1);
}

// ──────────────────────────────────────────────────────────────────────────────
// Backward compatibility: list_metadata_keys on unknown subscription
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_list_metadata_keys_unknown_subscription_returns_empty() {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(SubscriptionVault, ());
    let unknown = Bytes::from_slice(&env, b"unknown-sub");

    let keys = env.as_contract(&contract_id, || {
        SubscriptionVault::list_metadata_keys(env.clone(), unknown)
    });
    assert_eq!(keys.len(), 0);
}

// ──────────────────────────────────────────────────────────────────────────────
// Concurrency simulation: sequential writes on same subscription
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_sequential_concurrent_sets_are_consistent() {
    let s = Setup::new();

    let keys: StdVec<Bytes> = (0u32..5)
        .map(|i| Bytes::from_slice(&s.env, format!("c_key_{}", i).as_bytes()))
        .collect();

    for k in &keys {
        let k = k.clone();
        let val = s.bytes(b"concurrent_val");
        s.env.as_contract(&s.contract_id, || {
            SubscriptionVault::set_metadata(
                s.env.clone(),
                s.sub_id.clone(),
                s.subscriber.clone(),
                k,
                val,
            )
            .unwrap();
        });
    }

    assert_eq!(s.list().len() as usize, keys.len());
}

// ──────────────────────────────────────────────────────────────────────────────
// Key-length boundary: 31 bytes, 32 bytes, 33 bytes
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_key_31_bytes_accepted() {
    let s = Setup::new();
    let key = Bytes::from_array(&s.env, &[b'a'; 31]);
    let val = s.bytes(b"v");
    let result = s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            val,
        )
    });
    assert!(result.is_ok());
}

#[test]
fn test_value_255_bytes_accepted() {
    let s = Setup::new();
    let value = Bytes::from_array(&s.env, &[b'v'; 255]);
    let key = s.bytes(b"vtest");
    let result = s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            value,
        )
    });
    assert!(result.is_ok());
}

#[test]
fn test_value_256_bytes_accepted() {
    let s = Setup::new();
    let value = Bytes::from_array(&s.env, &[b'v'; 256]);
    let key = s.bytes(b"vtest256");
    let result = s.env.as_contract(&s.contract_id, || {
        SubscriptionVault::set_metadata(
            s.env.clone(),
            s.sub_id.clone(),
            s.subscriber.clone(),
            key,
            value,
        )
    });
    assert!(result.is_ok());
}

// ──────────────────────────────────────────────────────────────────────────────
// Multiple deletes of the same key fail on the second delete
// ──────────────────────────────────────────────────────────────────────────────

#[test]
fn test_double_delete_returns_not_found() {
    let s = Setup::new();
    s.set(b"once", b"v").unwrap();
    let sub = s.subscriber.clone();
    s.delete(&sub, b"once").unwrap();
    let err = s.delete(&sub, b"once").unwrap_err();
    assert_eq!(err, ContractError::MetadataKeyNotFound);
}
