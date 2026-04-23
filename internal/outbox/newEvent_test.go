package outbox

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Feature: outbox-hardening, Property 3: DedupeKey determinism
// Validates: Requirements 2.2
//
// Since occurredAt is captured inside NewEvent, we test the underlying
// computeDefaultDedupeKey helper directly to verify determinism.
func TestProperty3_DedupeKeyDeterminism(t *testing.T) {
	f := func(eventType, aggregateID string, nanos int64) bool {
		// Use a fixed time derived from the random nanos value.
		occurredAt := time.Unix(0, nanos)
		aggPtr := &aggregateID

		key1 := computeDefaultDedupeKey(eventType, aggPtr, occurredAt)
		key2 := computeDefaultDedupeKey(eventType, aggPtr, occurredAt)
		return key1 == key2
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// Feature: outbox-hardening, Property 3 (nil aggregateID variant)
// Validates: Requirements 2.2
func TestProperty3_DedupeKeyDeterminism_NilAggregateID(t *testing.T) {
	f := func(eventType string, nanos int64) bool {
		occurredAt := time.Unix(0, nanos)
		key1 := computeDefaultDedupeKey(eventType, nil, occurredAt)
		key2 := computeDefaultDedupeKey(eventType, nil, occurredAt)
		return key1 == key2
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Errorf("Property 3 (nil aggregateID) failed: %v", err)
	}
}

// Feature: outbox-hardening, Property 3 — different inputs produce different keys
// Validates: Requirements 2.2
func TestProperty3_DedupeKeyUniqueness(t *testing.T) {
	// Two calls with different occurred_at should produce different keys.
	t1 := time.Now()
	t2 := t1.Add(time.Nanosecond)
	aggID := "agg-1"

	key1 := computeDefaultDedupeKey("order.created", &aggID, t1)
	key2 := computeDefaultDedupeKey("order.created", &aggID, t2)
	assert.NotEqual(t, key1, key2, "different occurred_at should yield different dedupe keys")
}

// Feature: outbox-hardening, Property 4: Explicit DedupeKey passthrough
// Validates: Requirements 2.3
func TestProperty4_ExplicitDedupeKeyPassthrough(t *testing.T) {
	f := func(explicitKey string) bool {
		if explicitKey == "" {
			return true // skip empty — empty triggers default generation
		}
		event, err := NewEvent("test.event", map[string]string{"k": "v"}, nil, nil, explicitKey)
		if err != nil {
			return false
		}
		return event.DedupeKey == explicitKey
	}

	cfg := &quick.Config{
		MaxCount: 500,
		Rand:     rand.New(rand.NewSource(42)),
	}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// Feature: outbox-hardening, Property 4 — explicit key is stored verbatim (example test)
// Validates: Requirements 2.3
func TestProperty4_ExplicitDedupeKeyPassthrough_Example(t *testing.T) {
	explicit := "my-idempotency-key-abc123"
	event, err := NewEvent("order.placed", map[string]string{"order_id": "42"}, nil, nil, explicit)
	require.NoError(t, err)
	assert.Equal(t, explicit, event.DedupeKey)
}

// Feature: outbox-hardening, Property 4 — empty explicit key falls back to default
// Validates: Requirements 2.3
func TestProperty4_EmptyExplicitKeyFallsBackToDefault(t *testing.T) {
	event, err := NewEvent("order.placed", map[string]string{"order_id": "42"}, nil, nil, "")
	require.NoError(t, err)
	assert.NotEmpty(t, event.DedupeKey, "empty explicit key should fall back to default generation")
}

// Feature: outbox-hardening, Property 11: NewEvent applies sanitization
// Validates: Requirements 5.2
//
// For any event data map containing a PII field key, the event_data stored in
// the resulting Event SHALL not contain the original PII value.
func TestProperty11_NewEventAppliesSanitization(t *testing.T) {
	piiFields := DefaultPIIFieldBlocklist

	for _, field := range piiFields {
		field := field // capture
		t.Run("redacts_"+field, func(t *testing.T) {
			sensitiveValue := "super-secret-value-12345"
			data := map[string]interface{}{
				field:      sensitiveValue,
				"safe_key": "safe_value",
			}

			event, err := NewEvent("user.created", data, nil, nil)
			require.NoError(t, err)

			// The event_data is a JSON-encoded EventData wrapper.
			// Unmarshal the outer EventData, then inspect the inner Data.
			var ed EventData
			require.NoError(t, json.Unmarshal(event.EventData, &ed))

			// ed.Data is json.RawMessage — marshal it back to string for inspection.
			innerJSON, err := json.Marshal(ed.Data)
			require.NoError(t, err)

			assert.NotContains(t, string(innerJSON), sensitiveValue,
				"PII field %q value should be redacted in event_data", field)
			assert.Contains(t, string(innerJSON), "[REDACTED]",
				"PII field %q should be replaced with [REDACTED]", field)
		})
	}
}

// Feature: outbox-hardening, Property 11 — property-based variant
// Validates: Requirements 5.2
func TestProperty11_NewEventAppliesSanitization_Quick(t *testing.T) {
	f := func(sensitiveValue string) bool {
		if sensitiveValue == "" {
			return true
		}
		data := map[string]interface{}{
			"email":    sensitiveValue,
			"safe_key": "safe",
		}
		event, err := NewEvent("test.event", data, nil, nil)
		if err != nil {
			return false
		}
		var ed EventData
		if err := json.Unmarshal(event.EventData, &ed); err != nil {
			return false
		}
		innerJSON, err := json.Marshal(ed.Data)
		if err != nil {
			return false
		}
		// The original sensitive value must not appear in the inner JSON.
		return !jsonContains(string(innerJSON), sensitiveValue)
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 300}); err != nil {
		t.Errorf("Property 11 failed: %v", err)
	}
}

// jsonContains checks whether needle appears as a JSON string value in haystack.
func jsonContains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	// Marshal needle to get its JSON-escaped form (without surrounding quotes).
	encoded, err := json.Marshal(needle)
	if err != nil {
		return false
	}
	inner := string(encoded[1 : len(encoded)-1])
	return strings.Contains(haystack, inner)
}

func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
