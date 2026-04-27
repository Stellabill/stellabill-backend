// Package ingestion provides the contract event ingestion pipeline:
// consumer interface, parser, persistence adapter, and idempotent processing.
package ingestion

import (
	"context"
	"encoding/json"
	"time"
)

// Supported contract event types.
const (
	EventContractCreated   = "contract.created"
	EventContractAmended   = "contract.amended"
	EventContractRenewed   = "contract.renewed"
	EventContractCancelled = "contract.cancelled"
	EventContractExpired   = "contract.expired"
)

// validEventTypes is the set of recognised event types.
var validEventTypes = map[string]bool{
	EventContractCreated:   true,
	EventContractAmended:   true,
	EventContractRenewed:   true,
	EventContractCancelled: true,
	EventContractExpired:   true,
}

// RawEvent is the inbound event payload as received from external producers.
type RawEvent struct {
	IdempotencyKey string          `json:"idempotency_key"`
	EventType      string          `json:"event_type"`
	ContractID     string          `json:"contract_id"`
	TenantID       string          `json:"tenant_id"`
	OccurredAt     string          `json:"occurred_at"` // RFC 3339
	SequenceNum    int64           `json:"sequence_num"`
	Payload        json.RawMessage `json:"payload"`
}

// ContractEvent is the normalised, persisted read-model record.
type ContractEvent struct {
	ID             string          `json:"id"`
	IdempotencyKey string          `json:"idempotency_key"`
	EventType      string          `json:"event_type"`
	ContractID     string          `json:"contract_id"`
	TenantID       string          `json:"tenant_id"`
	Payload        json.RawMessage `json:"payload"`
	OccurredAt     time.Time       `json:"occurred_at"`
	IngestedAt     time.Time       `json:"ingested_at"`
	SequenceNum    int64           `json:"sequence_num"`
	Status         string          `json:"status"`
}

// EventConsumer defines the interface for consuming raw contract events.
type EventConsumer interface {
	Consume(ctx context.Context, raw RawEvent) (*ContractEvent, error)
}

// EventRepository is the persistence interface for contract events.
type EventRepository interface {
	Insert(ctx context.Context, event *ContractEvent) error
	ExistsByIdempotencyKey(ctx context.Context, key string) (bool, error)
	FindByID(ctx context.Context, id string) (*ContractEvent, error)
	ListByContractID(ctx context.Context, contractID string, limit, offset int) ([]*ContractEvent, error)
	LatestSequenceForContract(ctx context.Context, contractID string) (int64, error)
}

