package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type ContractEvent struct {
	EventType  string          `json:"event_type"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type SubscriptionCreatedData struct {
	SubscriptionID   string `json:"subscription_id"`
	CustomerID       string `json:"customer_id"`
	PlanID          string `json:"plan_id"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Interval        string `json:"interval"`
	Status          string `json:"status"`
	StartDate       string `json:"start_date"`
	CurrentPeriodEnd string `json:"current_period_end"`
}

type SubscriptionChargedData struct {
	SubscriptionID string `json:"subscription_id"`
	ChargeID      string `json:"charge_id"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	Description  string `json:"description"`
	InvoiceID    string `json:"invoice_id"`
}

type SubscriptionRefundedData struct {
	SubscriptionID      string `json:"subscription_id"`
	ChargeID           string `json:"charge_id"`
	RefundID           string `json:"refund_id"`
	Amount             int64  `json:"amount"`
	Currency           string `json:"currency"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	OriginalChargeAmount int64 `json:"original_charge_amount"`
}

func loadFixture(t *testing.T, filename string) ContractEvent {
	t.Helper()
	
	bytes, err := os.ReadFile(filepath.Join("..", "docs", "fixtures", filename))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", filename, err)
	}
	
	var event ContractEvent
	if err := json.Unmarshal(bytes, &event); err != nil {
		t.Fatalf("failed to parse fixture %s: %v", filename, err)
	}
	
	return event
}

func TestContractEvent_SubscriptionCreated(t *testing.T) {
	event := loadFixture(t, "subscription_created.json")
	
	if event.EventType != "subscription_created" {
		t.Errorf("event_type: want subscription_created, got %s", event.EventType)
	}
	if event.ID == "" {
		t.Error("id should not be empty")
	}
	
	var data SubscriptionCreatedData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	
	if data.SubscriptionID == "" {
		t.Error("subscription_id should not be empty")
	}
	if data.CustomerID == "" {
		t.Error("customer_id should not be empty")
	}
	if data.PlanID == "" {
		t.Error("plan_id should not be empty")
	}
	if data.Amount <= 0 {
		t.Error("amount should be positive")
	}
	if data.Currency == "" {
		t.Error("currency should not be empty")
	}
	if data.Interval == "" {
		t.Error("interval should not be empty")
	}
	if data.Status == "" {
		t.Error("status should not be empty")
	}
	if data.StartDate == "" {
		t.Error("start_date should not be empty")
	}
	if data.CurrentPeriodEnd == "" {
		t.Error("current_period_end should not be empty")
	}
}

func TestContractEvent_SubscriptionCharged(t *testing.T) {
	event := loadFixture(t, "subscription_charged.json")
	
	if event.EventType != "subscription_charged" {
		t.Errorf("event_type: want subscription_charged, got %s", event.EventType)
	}
	if event.ID == "" {
		t.Error("id should not be empty")
	}
	
	var data SubscriptionChargedData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	
	if data.SubscriptionID == "" {
		t.Error("subscription_id should not be empty")
	}
	if data.ChargeID == "" {
		t.Error("charge_id should not be empty")
	}
	if data.Amount <= 0 {
		t.Error("amount should be positive")
	}
	if data.Currency == "" {
		t.Error("currency should not be empty")
	}
	if data.Status == "" {
		t.Error("status should not be empty")
	}
}

func TestContractEvent_SubscriptionRefunded(t *testing.T) {
	event := loadFixture(t, "subscription_refunded.json")
	
	if event.EventType != "subscription_refunded" {
		t.Errorf("event_type: want subscription_refunded, got %s", event.EventType)
	}
	if event.ID == "" {
		t.Error("id should not be empty")
	}
	
	var data SubscriptionRefundedData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	
	if data.SubscriptionID == "" {
		t.Error("subscription_id should not be empty")
	}
	if data.ChargeID == "" {
		t.Error("charge_id should not be empty")
	}
	if data.RefundID == "" {
		t.Error("refund_id should not be empty")
	}
	if data.Amount <= 0 {
		t.Error("amount should be positive")
	}
	if data.Currency == "" {
		t.Error("currency should not be empty")
	}
	if data.Status == "" {
		t.Error("status should not be empty")
	}
}

func TestContractEvent_MissingRequiredFields(t *testing.T) {
	t.Run("missing_subscription_id", func(t *testing.T) {
		invalidJSON := `{"event_type":"subscription_created","id":"evt_001","timestamp":"2025-01-15T10:30:00Z","data":{"customer_id":"cust_001"}}`
		
		var event ContractEvent
		if err := json.Unmarshal([]byte(invalidJSON), &event); err != nil {
			t.Fatalf("should parse but be invalid: %v", err)
		}
		
		var data SubscriptionCreatedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("should unmarshal: %v", err)
		}
		
		if data.SubscriptionID != "" {
			t.Error("expected missing subscription_id to be empty")
		}
	})
	
	t.Run("missing_amount", func(t *testing.T) {
		invalidJSON := `{"event_type":"subscription_created","id":"evt_001","timestamp":"2025-01-15T10:30:00Z","data":{"subscription_id":"sub_001","amount":0}}`
		
		var event ContractEvent
		if err := json.Unmarshal([]byte(invalidJSON), &event); err != nil {
			t.Fatalf("should parse: %v", err)
		}
		
		var data SubscriptionCreatedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("should unmarshal: %v", err)
		}
		
		if data.Amount > 0 {
			t.Error("expected amount to be 0 for invalid event")
		}
	})
}

func TestContractEvent_UnknownEventName(t *testing.T) {
	unknownEvent := `{"event_type":"unknown_event","id":"evt_001","timestamp":"2025-01-15T10:30:00Z","data":{}}`
	
	var event ContractEvent
	if err := json.Unmarshal([]byte(unknownEvent), &event); err != nil {
		t.Fatalf("should parse unknown event: %v", err)
	}
	
	if event.EventType != "unknown_event" {
		t.Errorf("event_type should be preserved")
	}
}

func TestContractEvent_InvalidTypes(t *testing.T) {
	t.Run("string_for_number", func(t *testing.T) {
		invalidJSON := `{"event_type":"subscription_created","id":"evt_001","timestamp":"2025-01-15T10:30:00Z","data":{"subscription_id":"sub_001","amount":"not_a_number"}}`
		
		var event ContractEvent
		if err := json.Unmarshal([]byte(invalidJSON), &event); err != nil {
			t.Fatalf("should parse: %v", err)
		}
		
		var data SubscriptionCreatedData
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatalf("unmarshal should succeed with wrong type")
		}
		
		if data.Amount != 0 {
			t.Error("amount should be 0 when JSON string cannot be parsed as int")
		}
	})
	
	t.Run("number_for_string", func(t *testing.T) {
		invalidJSON := `{"event_type":"subscription_created","id":12345,"timestamp":"2025-01-15T10:30:00Z","data":{"subscription_id":"sub_001"}}`
		
		var event ContractEvent
		if err := json.Unmarshal([]byte(invalidJSON), &event); err != nil {
			t.Fatalf("should parse: %v", err)
		}
		
		if event.ID != "12345" && event.ID != "" {
			t.Logf("id might be converted: %s", event.ID)
		}
	})
}

func TestContractEvent_FixtureUpdateWorkflow(t *testing.T) {
	fixtures := []string{
		"subscription_created.json",
		"subscription_charged.json",
		"subscription_refunded.json",
	}
	
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			event := loadFixture(t, fixture)
			
			if event.EventType == "" {
				t.Error("event_type should not be empty")
			}
			if event.ID == "" {
				t.Error("id should not be empty")
			}
			if event.Timestamp == "" {
				t.Error("timestamp should not be empty")
			}
			if len(event.Data) == 0 {
				t.Error("data should not be empty")
			}
		})
	}
}