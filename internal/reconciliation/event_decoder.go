package reconciliation

import (
	"encoding/base64"
	"encoding/json"
)

type ContractEventType string

const (
	EventSubscriptionCreated ContractEventType = "subscription_created"
	EventSubscriptionUpdated ContractEventType = "subscription_updated"
	EventSubscriptionCanceled ContractEventType = "subscription_canceled"
	EventChargeCreated       ContractEventType = "charge_created"
	EventRefundCreated     ContractEventType = "refund_created"
)

type SorobanEvent struct {
	EventType ContractEventType `json:"type"`
	Topics    []string          `json:"topics"`
	Data      json.RawMessage  `json:"data"`
	Ledger    uint32            `json:"ledger"`
	TxHash    string            `json:"tx_hash"`
}

type SubscriptionCreatedData struct {
	SubscriptionID string `json:"subscription_id"`
	PlanID          string `json:"plan_id"`
	Customer        string `json:"customer"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	Interval        string `json:"interval"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
}

type SubscriptionUpdatedData struct {
	SubscriptionID string `json:"subscription_id"`
	Amount         string `json:"amount,omitempty"`
	Currency       string `json:"currency,omitempty"`
	Status        string `json:"status,omitempty"`
	Interval       string `json:"interval,omitempty"`
	UpdatedAt     int64  `json:"updated_at"`
}

type SubscriptionCanceledData struct {
	SubscriptionID string `json:"subscription_id"`
	CanceledAt    int64  `json:"canceled_at"`
	Reason        string `json:"reason,omitempty"`
}

type ChargeCreatedData struct {
	ChargeID        string `json:"charge_id"`
	SubscriptionID string `json:"subscription_id"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	CreatedAt       int64  `json:"created_at"`
	Status         string `json:"status"`
}

type RefundCreatedData struct {
	RefundID      string `json:"refund_id"`
	ChargeID     string `json:"charge_id"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Reason       string `json:"reason,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

type EventDecoder struct{}

func NewEventDecoder() *EventDecoder {
	return &EventDecoder{}
}

func (d *EventDecoder) DecodeEvent(eventData string) (*SorobanEvent, error) {
	decoded, err := base64.StdEncoding.DecodeString(eventData)
	if err != nil {
		return nil, err
	}

	var event SorobanEvent
	if err := json.Unmarshal(decoded, &event); err != nil {
		return nil, err
	}

	return &event, nil
}

func (d *EventDecoder) DecodeSubscriptionCreated(data json.RawMessage) (*SubscriptionCreatedData, error) {
	var result SubscriptionCreatedData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *EventDecoder) DecodeSubscriptionUpdated(data json.RawMessage) (*SubscriptionUpdatedData, error) {
	var result SubscriptionUpdatedData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *EventDecoder) DecodeSubscriptionCanceled(data json.RawMessage) (*SubscriptionCanceledData, error) {
	var result SubscriptionCanceledData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *EventDecoder) DecodeChargeCreated(data json.RawMessage) (*ChargeCreatedData, error) {
	var result ChargeCreatedData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *EventDecoder) DecodeRefundCreated(data json.RawMessage) (*RefundCreatedData, error) {
	var result RefundCreatedData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *EventDecoder) ValidateRequiredFields(event *SorobanEvent) error {
	if event.EventType == "" {
		return &ValidationError{Field: "type", Message: "event type is required"}
	}
	if len(event.Topics) == 0 {
		return &ValidationError{Field: "topics", Message: "topics are required"}
	}
	if event.Data == nil {
		return &ValidationError{Field: "data", Message: "data is required"}
	}
	return nil
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}