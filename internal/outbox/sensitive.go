package outbox

import (
	"encoding/json"
	"strings"
)

// DefaultSensitiveEventTypes lists event types that must be JWE-encrypted.
var DefaultSensitiveEventTypes = []string{
	"webhook.received",
	"payment.processed",
}

// SensitiveEventRegistry tracks which event types require JWE encryption.
type SensitiveEventRegistry struct {
	types map[string]struct{}
}

// NewSensitiveEventRegistry creates a registry from the given event type names.
func NewSensitiveEventRegistry(eventTypes []string) *SensitiveEventRegistry {
	if len(eventTypes) == 0 {
		eventTypes = DefaultSensitiveEventTypes
	}
	types := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		types[strings.TrimSpace(t)] = struct{}{}
	}
	return &SensitiveEventRegistry{types: types}
}

// IsSensitive reports whether the event type requires JWE encryption.
func (r *SensitiveEventRegistry) IsSensitive(eventType string) bool {
	if r == nil {
		return false
	}
	_, ok := r.types[eventType]
	return ok
}

// ResolveSubscriberID extracts the subscriber identifier from an outbox event.
func ResolveSubscriberID(event *Event) string {
	if event == nil {
		return ""
	}
	if event.AggregateType != nil && event.AggregateID != nil &&
		strings.EqualFold(*event.AggregateType, "subscriber") && *event.AggregateID != "" {
		return *event.AggregateID
	}

	var envelope EventData
	if err := json.Unmarshal(event.EventData, &envelope); err == nil {
		if envelope.SubscriberID != "" {
			return envelope.SubscriberID
		}
		if dataMap, ok := envelope.Data.(map[string]interface{}); ok {
			if sid, ok := dataMap["subscriber_id"].(string); ok && sid != "" {
				return sid
			}
			if cid, ok := dataMap["customer_id"].(string); ok && cid != "" {
				return cid
			}
		}
	}
	return ""
}
