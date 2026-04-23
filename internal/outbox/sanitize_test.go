package outbox

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
)

// Feature: outbox-hardening, Property 10: SanitizePayload redacts all blocklist fields

// TestSanitizePayload_Property10 uses testing/quick to verify that for any map
// containing blocklist keys, those keys are redacted and non-blocklist keys are
// preserved.
//
// Validates: Requirements 5.1, 5.4
func TestSanitizePayload_Property10(t *testing.T) {
	blocklist := []string{"email", "phone", "ssn", "card_number", "password", "token", "secret"}

	// Build a lowercase set for assertions.
	blockSet := make(map[string]struct{}, len(blocklist))
	for _, k := range blocklist {
		blockSet[strings.ToLower(k)] = struct{}{}
	}

	// property: for any map[string]string, blocklist keys are redacted and
	// non-blocklist keys retain their original values.
	property := func(input map[string]string) bool {
		if len(input) == 0 {
			return true // nothing to check
		}

		result, err := SanitizePayload(input, blocklist)
		if err != nil {
			t.Logf("SanitizePayload error: %v", err)
			return false
		}

		var out map[string]json.RawMessage
		if err := json.Unmarshal(result, &out); err != nil {
			t.Logf("unmarshal error: %v", err)
			return false
		}

		for key, rawVal := range out {
			if _, isBlocked := blockSet[strings.ToLower(key)]; isBlocked {
				// Must be "[REDACTED]"
				var s string
				if err := json.Unmarshal(rawVal, &s); err != nil || s != "[REDACTED]" {
					t.Logf("key %q: expected [REDACTED], got %s", key, rawVal)
					return false
				}
			} else {
				// Must match original value.
				original := input[key]
				var got string
				if err := json.Unmarshal(rawVal, &got); err != nil || got != original {
					t.Logf("key %q: expected %q, got %s", key, original, rawVal)
					return false
				}
			}
		}
		return true
	}

	if err := quick.Check(property, nil); err != nil {
		t.Errorf("Property 10 failed: %v", err)
	}
}

// TestSanitizePayload_NonMap verifies that non-object JSON (array, string,
// number) is returned unchanged.
func TestSanitizePayload_NonMap(t *testing.T) {
	blocklist := []string{"email", "password"}

	cases := []interface{}{
		[]string{"email", "password"},
		"email",
		42,
		3.14,
		true,
	}

	for _, input := range cases {
		expected, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}

		got, err := SanitizePayload(input, blocklist)
		if err != nil {
			t.Errorf("SanitizePayload(%v) unexpected error: %v", input, err)
			continue
		}

		if string(got) != string(expected) {
			t.Errorf("SanitizePayload(%v): expected %s, got %s", input, expected, got)
		}
	}
}
