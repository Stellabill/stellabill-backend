package ingestion

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"stellarbill-backend/internal/timeutil"
)

// Parser errors.
var (
	ErrMissingIdempotencyKey = errors.New("missing idempotency_key")
	ErrMissingEventType      = errors.New("missing event_type")
	ErrInvalidEventType      = errors.New("invalid event_type")
	ErrMissingContractID     = errors.New("missing contract_id")
	ErrMissingTenantID       = errors.New("missing tenant_id")
	ErrMissingOccurredAt     = errors.New("missing occurred_at")
	ErrInvalidOccurredAt     = errors.New("invalid occurred_at: must be RFC 3339")
	ErrFutureOccurredAt      = errors.New("occurred_at cannot be in the future")
	ErrInvalidPayload        = errors.New("invalid payload: must be valid JSON object")
	ErrNegativeSequence      = errors.New("sequence_num must be non-negative")
)

// ParseResult holds the validated output of parsing a RawEvent.
type ParseResult struct {
	IdempotencyKey string
	EventType      string
	ContractID     string
	TenantID       string
	OccurredAt     time.Time
	SequenceNum    int64
	Payload        json.RawMessage
}

// Parse validates and normalises a RawEvent into a ParseResult.
func Parse(raw RawEvent) (*ParseResult, error) {
	key := strings.TrimSpace(raw.IdempotencyKey)
	if key == "" {
		return nil, ErrMissingIdempotencyKey
	}

	eventType := strings.TrimSpace(raw.EventType)
	if eventType == "" {
		return nil, ErrMissingEventType
	}
	if !validEventTypes[eventType] {
		return nil, ErrInvalidEventType
	}

	contractID := strings.TrimSpace(raw.ContractID)
	if contractID == "" {
		return nil, ErrMissingContractID
	}

	tenantID := strings.TrimSpace(raw.TenantID)
	if tenantID == "" {
		return nil, ErrMissingTenantID
	}

	if strings.TrimSpace(raw.OccurredAt) == "" {
		return nil, ErrMissingOccurredAt
	}
	occurredAt, err := timeutil.ParseRFC3339ToUTC(raw.OccurredAt)
	if err != nil {
		return nil, ErrInvalidOccurredAt
	}
	if occurredAt.After(timeutil.NowUTC().Add(5 * time.Minute)) {
		return nil, ErrFutureOccurredAt
	}

	if raw.SequenceNum < 0 {
		return nil, ErrNegativeSequence
	}

	payload := raw.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	} else {
		var obj map[string]interface{}
		if err := json.Unmarshal(payload, &obj); err != nil {
			return nil, ErrInvalidPayload
		}
	}

	return &ParseResult{
		IdempotencyKey: key,
		EventType:      eventType,
		ContractID:     contractID,
		TenantID:       tenantID,
		OccurredAt:     occurredAt,
		SequenceNum:    raw.SequenceNum,
		Payload:        payload,
	}, nil
}
