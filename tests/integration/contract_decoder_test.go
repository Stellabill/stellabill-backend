package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"stellarbill-backend/internal/ingestion"
)

func TestContractEventDecoding(t *testing.T) {
	fixtures, err := loadContractFixtures()
	if err != nil {
		t.Fatalf("failed to load fixtures: %v", err)
	}

	for _, fixture := range fixtures {
		t.Run(fixture.Description, func(t *testing.T) {
			raw := ingestion.RawEvent{
				IdempotencyKey: fixture.Event.IdempotencyKey,
				EventType:      fixture.Event.EventType,
				ContractID:     fixture.Event.ContractID,
				TenantID:       fixture.Event.TenantID,
				OccurredAt:     fixture.Event.OccurredAt,
				SequenceNum:    fixture.Event.SequenceNum,
			}

			if fixture.Event.Payload != nil {
				raw.Payload, _ = json.Marshal(fixture.Event.Payload)
			}

			result, err := ingestion.Parse(raw)
			if err != nil {
				t.Fatalf("failed to parse event: %v", err)
			}

			if result.IdempotencyKey != fixture.Event.IdempotencyKey {
				t.Errorf("idempotency key mismatch: got %s, want %s", result.IdempotencyKey, fixture.Event.IdempotencyKey)
			}
			if result.EventType != fixture.Event.EventType {
				t.Errorf("event type mismatch: got %s, want %s", result.EventType, fixture.Event.EventType)
			}
			if result.ContractID != fixture.Event.ContractID {
				t.Errorf("contract id mismatch: got %s, want %s", result.ContractID, fixture.Event.ContractID)
			}
			if result.TenantID != fixture.Event.TenantID {
				t.Errorf("tenant id mismatch: got %s, want %s", result.TenantID, fixture.Event.TenantID)
			}
		})
	}
}

func TestContractEventMissingFields(t *testing.T) {
	tests := []struct {
		name        string
		raw         ingestion.RawEvent
		expectError error
	}{
		{
			name: "missing idempotency_key",
			raw: ingestion.RawEvent{
				IdempotencyKey: "",
				EventType:      ingestion.EventContractCreated,
				ContractID:     "contract_123",
				TenantID:        "tenant_1",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    1,
			},
			expectError: ingestion.ErrMissingIdempotencyKey,
		},
		{
			name: "missing event_type",
			raw: ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      "",
				ContractID:     "contract_123",
				TenantID:        "tenant_1",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    1,
			},
			expectError: ingestion.ErrMissingEventType,
		},
		{
			name: "missing contract_id",
			raw: ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:     "",
				TenantID:        "tenant_1",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    1,
			},
			expectError: ingestion.ErrMissingContractID,
		},
		{
			name: "missing tenant_id",
			raw: ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:     "contract_123",
				TenantID:       "",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    1,
			},
			expectError: ingestion.ErrMissingTenantID,
		},
		{
			name: "missing occurred_at",
			raw: ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:     "contract_123",
				TenantID:       "tenant_1",
				OccurredAt:     "",
				SequenceNum:    1,
			},
			expectError: ingestion.ErrMissingOccurredAt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ingestion.Parse(tt.raw)
			if err != tt.expectError {
				t.Errorf("expected error %v, got %v", tt.expectError, err)
			}
		})
	}
}

func TestContractEventInvalidEventType(t *testing.T) {
	raw := ingestion.RawEvent{
		IdempotencyKey: "key_001",
		EventType:      "contract.invalid_type",
		ContractID:    "contract_123",
		TenantID:      "tenant_1",
		OccurredAt:    time.Now().Format(time.RFC3339),
		SequenceNum:  1,
	}

	_, err := ingestion.Parse(raw)
	if err != ingestion.ErrInvalidEventType {
		t.Errorf("expected ErrInvalidEventType, got %v", err)
	}
}

func TestContractEventMalformedPayload(t *testing.T) {
	tests := []struct {
		name        string
		payload    string
		expectErr  error
	}{
		{
			name:       "invalid JSON",
			payload:    `not valid json`,
			expectErr: ingestion.ErrInvalidPayload,
		},
		{
			name:       "JSON array instead of object",
			payload:    `[1, 2, 3]`,
			expectErr: ingestion.ErrInvalidPayload,
		},
		{
			name:       "JSON number",
			payload:    `123`,
			expectErr: ingestion.ErrInvalidPayload,
		},
		{
			name:       "JSON string",
			payload:    `"string"`,
			expectErr: ingestion.ErrInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:     "contract_123",
				TenantID:       "tenant_1",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    1,
				Payload:        json.RawMessage(tt.payload),
			}

			_, err := ingestion.Parse(raw)
			if err != tt.expectErr {
				t.Errorf("expected error %v, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestContractEventSequenceNumberValidation(t *testing.T) {
	tests := []struct {
		name        string
		sequence   int64
		expectErr  error
	}{
		{
			name:       "negative sequence",
			sequence:  -1,
			expectErr: ingestion.ErrNegativeSequence,
		},
		{
			name:       "zero sequence is valid",
			sequence:  0,
			expectErr:  nil,
		},
		{
			name:       "positive sequence is valid",
			sequence:  100,
			expectErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:    "contract_123",
				TenantID:       "tenant_1",
				OccurredAt:     time.Now().Format(time.RFC3339),
				SequenceNum:    tt.sequence,
			}

			_, err := ingestion.Parse(raw)
			if err != tt.expectErr {
				t.Errorf("expected error %v, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestContractEventOccurredAtValidation(t *testing.T) {
	tests := []struct {
		name        string
		occurredAt  string
		expectErr error
	}{
		{
			name:       "invalid RFC3339",
			occurredAt: "2024/01/15",
			expectErr: ingestion.ErrInvalidOccurredAt,
		},
		{
			name:       "unix timestamp",
			occurredAt: "1705315800",
			expectErr: ingestion.ErrInvalidOccurredAt,
		},
		{
			name:       "ISO 8601 without timezone",
			occurredAt: "2024-01-15T10:30:00",
			expectErr: ingestion.ErrInvalidOccurredAt,
		},
		{
			name:       "valid RFC3339 with Z",
			occurredAt: "2024-01-15T10:30:00Z",
			expectErr: nil,
		},
		{
			name:       "valid RFC3339 with offset",
			occurredAt: "2024-01-15T10:30:00+00:00",
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := ingestion.RawEvent{
				IdempotencyKey: "key_001",
				EventType:      ingestion.EventContractCreated,
				ContractID:    "contract_123",
				TenantID:       "tenant_1",
				OccurredAt:    tt.occurredAt,
				SequenceNum:   1,
			}

			_, err := ingestion.Parse(raw)
			if err != tt.expectErr {
				t.Errorf("expected error %v, got %v", tt.expectErr, err)
			}
		})
	}
}

type ContractEventFixture struct {
	Description string                  `json:"description"`
	Event       FixtureEvent             `json:"event"`
}

type FixtureEvent struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	EventType     string                 `json:"event_type"`
	ContractID   string                 `json:"contract_id"`
	TenantID     string                 `json:"tenant_id"`
	OccurredAt   string                 `json:"occurred_at"`
	SequenceNum  int64                  `json:"sequence_num"`
	Payload      map[string]interface{} `json:"payload"`
}

func loadContractFixtures() ([]ContractEventFixture, error) {
	// Look for fixtures in multiple locations
	paths := []string{
		"tests/integration/fixtures/contract_events.json",
		"../tests/integration/fixtures/contract_events.json",
		"../../tests/integration/fixtures/contract_events.json",
	}

	var data []byte
	var err error

	for _, path := range paths {
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}

	if data == nil {
		return nil, err
	}

	var fixtures []ContractEventFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, err
	}

	return fixtures, nil
}

func TestContractEventFixtureFilesExist(t *testing.T) {
	baseDir := filepath.Join("tests", "integration", "fixtures")
	fixtureFiles := []string{
		"contract_events.json",
	}

	for _, fixture := range fixtureFiles {
		path := filepath.Join(baseDir, fixture)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Logf("Fixture file note: %s should be updated from contracts repo", path)
		}
	}
}