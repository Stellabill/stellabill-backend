package outbox

import (
	"context"
	"encoding/json"
	"fmt"
)

// JWEPublisher wraps a publisher and encrypts sensitive event payloads before delivery.
type JWEPublisher struct {
	inner     Publisher
	keys      SubscriberKeyRepository
	encryptor *JWEEncryptor
	sensitive *SensitiveEventRegistry
}

// NewJWEPublisher creates a publisher that applies JWE to sensitive events.
func NewJWEPublisher(inner Publisher, keys SubscriberKeyRepository, encryptor *JWEEncryptor, sensitive *SensitiveEventRegistry) Publisher {
	return &JWEPublisher{
		inner:     inner,
		keys:      keys,
		encryptor: encryptor,
		sensitive: sensitive,
	}
}

// Publish encrypts sensitive payloads with the subscriber JWK before delegating.
func (p *JWEPublisher) Publish(ctx context.Context, event *Event) error {
	if p.sensitive == nil || !p.sensitive.IsSensitive(event.EventType) {
		return p.inner.Publish(ctx, event)
	}

	subscriberID := ResolveSubscriberID(event)
	if subscriberID == "" {
		return &PermanentPublishError{Reason: "missing subscriber id for sensitive event"}
	}

	key, err := p.keys.GetActiveKey(subscriberID)
	if err != nil {
		return &PermanentPublishError{Reason: "missing subscriber encryption key", Err: err}
	}

	var envelope EventData
	if err := json.Unmarshal(event.EventData, &envelope); err != nil {
		return fmt.Errorf("unmarshal event data: %w", err)
	}

	payload := map[string]interface{}{
		"id":             event.ID,
		"type":           event.EventType,
		"data":           envelope.Data,
		"occurred_at":    event.OccurredAt,
		"aggregate_id":   event.AggregateID,
		"aggregate_type": event.AggregateType,
		"version":        event.Version,
		"subscriber_id":  subscriberID,
	}

	compact, err := p.encryptor.Encrypt(payload, key.JWK)
	if err != nil {
		return fmt.Errorf("encrypt sensitive payload: %w", err)
	}

	encryptedEnvelope := EventData{
		Type:         event.EventType,
		Timestamp:    envelope.Timestamp,
		ID:           envelope.ID,
		Encrypted:    true,
		JWE:          compact,
		KeyID:        key.KeyID,
		SubscriberID: subscriberID,
	}
	encryptedJSON, err := json.Marshal(encryptedEnvelope)
	if err != nil {
		return fmt.Errorf("marshal encrypted envelope: %w", err)
	}

	encryptedEvent := *event
	encryptedEvent.EventData = json.RawMessage(encryptedJSON)
	return p.inner.Publish(ctx, &encryptedEvent)
}

// PrepareEncryptedEventData encrypts sensitive event data for at-rest storage.
func PrepareEncryptedEventData(eventType string, data interface{}, subscriberID string, keys SubscriberKeyRepository, encryptor *JWEEncryptor, sensitive *SensitiveEventRegistry) (json.RawMessage, error) {
	eventData := EventData{
		Type:         eventType,
		Data:         data,
		Timestamp:    timeNow(),
		ID:           newEventDataID(),
		SubscriberID: subscriberID,
	}

	if sensitive == nil || !sensitive.IsSensitive(eventType) {
		return json.Marshal(eventData)
	}

	if subscriberID == "" {
		return nil, &PermanentPublishError{Reason: "missing subscriber id for sensitive event"}
	}

	key, err := keys.GetActiveKey(subscriberID)
	if err != nil {
		return nil, &PermanentPublishError{Reason: "missing subscriber encryption key", Err: err}
	}

	compact, err := encryptor.Encrypt(data, key.JWK)
	if err != nil {
		return nil, fmt.Errorf("encrypt event data: %w", err)
	}

	eventData.Encrypted = true
	eventData.JWE = compact
	eventData.KeyID = key.KeyID
	eventData.Data = nil
	return json.Marshal(eventData)
}
