package reconciliation

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDecodeSubscriptionCreatedEvent(t *testing.T) {
	decoder := NewEventDecoder()

	eventData := "eyJ0eXBlIjoic3Vic2NyaXB0aW9uX2NyZWF0ZWQiLCJ0b3BpY3MiOlsiZ29vZ2xlLXVzZXItb25lIiwiY3VzdG9tZXIiLCJzdWItMTAwMSJdLCJkYXRhIjp7InN1YnNjcmlwdGlvbl9pZCI6InN1Yi0xMDAxIiwicGxhbl9pZCI6InBsYW4tMTAwMSIsImN1c3RvbWVyIjoiZ29vZ2xlLXVzZXItb25lIiwiYW1vdW50IjoiOTk5MCIsImN1cnJlbmN5IjoiVVNEIiwiaW50ZXJ2YWwiOiJtb250aCIsInN0YXR1cyI6ImFjdGl2ZSIsImNyZWF0ZWRBdCI6MTcwMDAwMDAwMDB9LCJsZWRnZXIiOjMwMDAwLCJ0eF9oYXNoIjoiMHhkMTIzNDU2Nzg5YWJjZGVmZzEyMzQ1Njc4OWFiY2RlZmcyMTIzNDU2Nzh4eXoifQ=="

	event, err := decoder.DecodeEvent(eventData)
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if event.EventType != EventSubscriptionCreated {
		t.Errorf("expected event type subscription_created, got %s", event.EventType)
	}

	if len(event.Topics) != 3 {
		t.Errorf("expected 3 topics, got %d", len(event.Topics))
	}

	data, err := decoder.DecodeSubscriptionCreated(event.Data)
	if err != nil {
		t.Fatalf("failed to decode subscription created data: %v", err)
	}

	if data.SubscriptionID != "sub-1001" {
		t.Errorf("expected subscription_id 'sub-1001', got %s", data.SubscriptionID)
	}

	if data.Amount != "9990" {
		t.Errorf("expected amount 9990, got %s", data.Amount)
	}

	if data.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", data.Currency)
	}

	if data.Interval != "month" {
		t.Errorf("expected interval month, got %s", data.Interval)
	}

	if data.Status != "active" {
		t.Errorf("expected status active, got %s", data.Status)
	}
}

func TestDecodeSubscriptionUpdatedEvent(t *testing.T) {
	decoder := NewEventDecoder()

	eventData := "eyJ0eXBlIjoic3Vic2NyaXB0aW9uX3VwZGF0ZWQiLCJ0b3BpY3MiOlsiZ29vZ2xlLXVzZXItb25lIiwiY3VzdG9tZXIiLCJzdWItMTAwMSJdLCJkYXRhIjp7InN1YnNjcmlwdGlvbl9pZCI6InN1Yi0xMDAxIiwiYW1vdW50IjoiMTQ5OTAiLCJjdXJyZW5jeSI6IlVTRCIsInN0YXR1cyI6ImNyZWRpdF91cGdyYWRlIiwiaW50ZXJ2YWwiOiJtb250aCIsInVwZGF0ZWRBdCI6MTcwMDAwMDAwNTB9LCJsZWRnZXIiOjMwMDAyLCJ0eF9oYXNoIjoiMHhmMzM0NTY3ODlhYmNkZWZnMzM0NTY3ODlhYmNkZWZnMTMzNDU2Nzg5YWJjZGVmZzMxNDM1Njc4OXh5eiJ9"

	event, err := decoder.DecodeEvent(eventData)
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if event.EventType != EventSubscriptionUpdated {
		t.Errorf("expected event type subscription_updated, got %s", event.EventType)
	}

	data, err := decoder.DecodeSubscriptionUpdated(event.Data)
	if err != nil {
		t.Fatalf("failed to decode subscription updated data: %v", err)
	}

	if data.SubscriptionID != "sub-1001" {
		t.Errorf("expected subscription_id 'sub-1001', got %s", data.SubscriptionID)
	}

	if data.Amount != "14990" {
		t.Errorf("expected amount 14990, got %s", data.Amount)
	}

	if data.Status != "credit_upgrade" {
		t.Errorf("expected status credit_upgrade, got %s", data.Status)
	}
}

func TestDecodeSubscriptionCanceledEvent(t *testing.T) {
	decoder := NewEventDecoder()

	eventData := "eyJ0eXBlIjoic3Vic2NyaXB0aW9uX2NhbmNlbGVkIiwidG9waWNzIjpbImdvb2dsZS11c2VyLW9uZSIsImN1c3RvbWVyIiwic3ViLTEwMDEiXSwiZGF0YSI6eyJzdWJzY3JpcHRpb25faWQiOiJzdWItMTAwMSIsImNhbmNlbGVkQXQiOjE3MDAwMDAwMTAwLCJyZWFzb24iOiJjdXN0b21lcl9yZXF1ZXN0In0sImxlZGdlIjozMDAwMywidHhfaGFzaCI6IjB4ZDQzNDU2Nzg5YWJjZGVmZzQ0NTY3ODlhbWJlY2RlZmc0NTY3ODlhYmNlZGYzNTQ2Nzg5eXoifQ=="

	event, err := decoder.DecodeEvent(eventData)
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if event.EventType != EventSubscriptionCanceled {
		t.Errorf("expected event type subscription_canceled, got %s", event.EventType)
	}

	data, err := decoder.DecodeSubscriptionCanceled(event.Data)
	if err != nil {
		t.Fatalf("failed to decode subscription canceled data: %v", err)
	}

	if data.SubscriptionID != "sub-1001" {
		t.Errorf("expected subscription_id 'sub-1001', got %s", data.SubscriptionID)
	}

	if data.Reason != "customer_request" {
		t.Errorf("expected reason customer_request, got %s", data.Reason)
	}
}

func TestDecodeChargeCreatedEvent(t *testing.T) {
	decoder := NewEventDecoder()

	eventData := "eyJ0eXBlIjoiY2hhcmdlX2NyZWF0ZWQiLCJ0b3BpY3MiOlsiZ29vZ2xlLXVzZXItb25lIiwiY3VzdG9tZXIiLCJzdWItMTAwMSIsImNyZy0xXzEyMzQiXSwiZGF0YSI6eyJjaGFyZ2VfaWQiOiJjcmctMV8xMjM0Iiwic3Vic2NyaXB0aW9uX2lkIjoic3ViLTEwMDEiLCJhbW91bnQiOiI5OTkwIiwiY3VycmVuY3kiOiJVU0QiLCJjcmVhdGVkQXQiOjE3MDAwMDAwMDIwMCwic3RhdHVzIjoic2xpcHBlZCJ9LCJsZWRnZXIiOjMwMDA0LCJ0eF9oYXNoIjoiMHhlNTM0NTY3ODlhYmNkZWZnNTU0NTY3ODlhYmNkZWZnNTU0NTY3ODl4eXoifQ=="

	event, err := decoder.DecodeEvent(eventData)
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if event.EventType != EventChargeCreated {
		t.Errorf("expected event type charge_created, got %s", event.EventType)
	}

	data, err := decoder.DecodeChargeCreated(event.Data)
	if err != nil {
		t.Fatalf("failed to decode charge created data: %v", err)
	}

	if data.ChargeID != "crg-1_1234" {
		t.Errorf("expected charge_id 'crg-1_1234', got %s", data.ChargeID)
	}

	if data.Amount != "9990" {
		t.Errorf("expected amount 9990, got %s", data.Amount)
	}

	if data.Status != "slipped" {
		t.Errorf("expected status slipped, got %s", data.Status)
	}
}

func TestDecodeRefundCreatedEvent(t *testing.T) {
	decoder := NewEventDecoder()

	eventData := "eyJ0eXBlIjoicmVmdW5kX2NyZWF0ZWQiLCJ0b3BpY3MiOlsiZ29vZ2xlLXVzZXItb25lIiwiY3VzdG9tZXIiLCJzdWItMTAwMSIsImNyZy0xXzEyMzQiLCJyZWYtMV8yNTYzIl0sImRhdGEiOnsicmVmdW5kX2lkIjoicmVmLTVfMjU2MyIsImNoYXJnZV9pZCI6ImNyZy0xXzEyMzQiLCJhbW91bnQiOiI0OTk1IiwiY3VycmVuY3kiOiJVU0QiLCJyZWFzb24iOiJjcmVkaXRfdXBncmFkZSIsImNyZWF0ZWRBdCI6MTcwMDAwMDAwNTAwfSwibGVkZ2VyIjozMDAwNSwidHhfaGFzaCI6IjB4ZjY1NDU2Nzg5YWJjZGVmZzY1NDU2Nzg5YWJjZGVmZzY1NDU2Nzg5eXoifQ=="

	event, err := decoder.DecodeEvent(eventData)
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if event.EventType != EventRefundCreated {
		t.Errorf("expected event type refund_created, got %s", event.EventType)
	}

	data, err := decoder.DecodeRefundCreated(event.Data)
	if err != nil {
		t.Fatalf("failed to decode refund created data: %v", err)
	}

	if data.RefundID != "ref-5_2563" {
		t.Errorf("expected refund_id 'ref-5_2563', got %s", data.RefundID)
	}

	if data.Amount != "4995" {
		t.Errorf("expected amount 4995, got %s", data.Amount)
	}

	if data.Reason != "credit_upgrade" {
		t.Errorf("expected reason credit_upgrade, got %s", data.Reason)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	decoder := NewEventDecoder()

	tests := []struct {
		name      string
		event     *SorobanEvent
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid event",
			event: &SorobanEvent{
				EventType: EventSubscriptionCreated,
				Topics:    []string{"topic1"},
				Data:      json.RawMessage(`{}`),
			},
			wantError: false,
		},
		{
			name: "missing type",
			event: &SorobanEvent{
				Topics: []string{"topic1"},
				Data:   json.RawMessage(`{}`),
			},
			wantError: true,
			errorMsg:  "event type is required",
		},
		{
			name: "missing topics",
			event: &SorobanEvent{
				EventType: EventSubscriptionCreated,
				Data:     json.RawMessage(`{}`),
			},
			wantError: true,
			errorMsg:  "topics are required",
		},
		{
			name: "missing data",
			event: &SorobanEvent{
				EventType: EventSubscriptionCreated,
				Topics:   []string{"topic1"},
			},
			wantError: true,
			errorMsg:  "data is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decoder.ValidateRequiredFields(tt.event)
			if tt.wantError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantError && err != nil && tt.errorMsg != "" {
				if err.Error() != tt.errorMsg && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestLoadFixtures(t *testing.T) {
	fixtureFile := "fixtures/soroban_events.json"

	data, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatalf("failed to read fixture file: %v", err)
	}

	var fixtures map[string]interface{}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("failed to parse fixture JSON: %v", err)
	}

	if _, ok := fixtures["subscription_created_events"]; !ok {
		t.Error("expected subscription_created_events in fixtures")
	}

	if _, ok := fixtures["subscription_updated_events"]; !ok {
		t.Error("expected subscription_updated_events in fixtures")
	}

	if _, ok := fixtures["subscription_canceled_events"]; !ok {
		t.Error("expected subscription_canceled_events in fixtures")
	}

	if _, ok := fixtures["charge_created_events"]; !ok {
		t.Error("expected charge_created_events in fixtures")
	}

	if _, ok := fixtures["refund_created_events"]; !ok {
		t.Error("expected refund_created_events in fixtures")
	}

	if _, ok := fixtures["malformed_events"]; !ok {
		t.Error("expected malformed_events in fixtures")
	}
}

func TestMalformedEventRejection(t *testing.T) {
	decoder := NewEventDecoder()

	malformedEvents := []struct {
		name          string
		eventData    string
		expectError bool
	}{
		{
			name:          "invalid base64",
			eventData:      "NOT_VALID_JSON",
			expectError:   true,
		},
		{
			name:          "empty event data",
			eventData:      "",
			expectError:   true,
		},
	}

	for _, tt := range malformedEvents {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decoder.DecodeEvent(tt.eventData)
			if tt.expectError && err == nil {
				t.Errorf("expected error for malformed event")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}