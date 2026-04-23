package outbox

import (
	"encoding/json"
	"strings"
)

// SanitizePayload marshals data to JSON and replaces any top-level key that
// matches a blocklist entry (case-insensitive) with the string "[REDACTED]".
// If data is not a JSON object (e.g. array, string, number), the marshalled
// JSON is returned unchanged (best-effort).
func SanitizePayload(data interface{}, blocklist []string) (json.RawMessage, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Try to unmarshal as a generic map; if it's not an object, return as-is.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		// Not a JSON object — return unchanged.
		return raw, nil
	}

	// Build a lowercase set of blocklist entries for O(1) lookup.
	blocked := make(map[string]struct{}, len(blocklist))
	for _, key := range blocklist {
		blocked[strings.ToLower(key)] = struct{}{}
	}

	// Replace matching keys with "[REDACTED]".
	for key := range obj {
		if _, found := blocked[strings.ToLower(key)]; found {
			obj[key] = json.RawMessage(`"[REDACTED]"`)
		}
	}

	sanitized, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return sanitized, nil
}
