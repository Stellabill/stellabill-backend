package ingestion

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRawEvent() RawEvent {
	return RawEvent{
		IdempotencyKey: "key-001",
		EventType:      EventContractCreated,
		ContractID:     "contract-abc",
		TenantID:       "tenant-1",
		OccurredAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		SequenceNum:    1,
		Payload:        json.RawMessage(`{"amount":1000}`),
	}
}

func TestParse_Valid(t *testing.T) {
	raw := validRawEvent()
	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "key-001", result.IdempotencyKey)
	assert.Equal(t, EventContractCreated, result.EventType)
	assert.Equal(t, "contract-abc", result.ContractID)
	assert.Equal(t, "tenant-1", result.TenantID)
	assert.Equal(t, int64(1), result.SequenceNum)
	assert.False(t, result.OccurredAt.IsZero())
}

func TestParse_AllEventTypes(t *testing.T) {
	for _, et := range []string{
		EventContractCreated,
		EventContractAmended,
		EventContractRenewed,
		EventContractCancelled,
		EventContractExpired,
	} {
		t.Run(et, func(t *testing.T) {
			raw := validRawEvent()
			raw.EventType = et
			result, err := Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, et, result.EventType)
		})
	}
}

func TestParse_MissingIdempotencyKey(t *testing.T) {
	raw := validRawEvent()
	raw.IdempotencyKey = ""
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingIdempotencyKey)
}

func TestParse_WhitespaceOnlyIdempotencyKey(t *testing.T) {
	raw := validRawEvent()
	raw.IdempotencyKey = "   "
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingIdempotencyKey)
}

func TestParse_MissingEventType(t *testing.T) {
	raw := validRawEvent()
	raw.EventType = ""
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingEventType)
}

func TestParse_InvalidEventType(t *testing.T) {
	raw := validRawEvent()
	raw.EventType = "contract.unknown"
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidEventType)
}

func TestParse_MissingContractID(t *testing.T) {
	raw := validRawEvent()
	raw.ContractID = ""
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingContractID)
}

func TestParse_MissingTenantID(t *testing.T) {
	raw := validRawEvent()
	raw.TenantID = ""
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingTenantID)
}

func TestParse_MissingOccurredAt(t *testing.T) {
	raw := validRawEvent()
	raw.OccurredAt = ""
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrMissingOccurredAt)
}

func TestParse_InvalidOccurredAt(t *testing.T) {
	raw := validRawEvent()
	raw.OccurredAt = "not-a-date"
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidOccurredAt)
}

func TestParse_FutureOccurredAt(t *testing.T) {
	raw := validRawEvent()
	raw.OccurredAt = time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrFutureOccurredAt)
}

func TestParse_NegativeSequence(t *testing.T) {
	raw := validRawEvent()
	raw.SequenceNum = -1
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrNegativeSequence)
}

func TestParse_ZeroSequence(t *testing.T) {
	raw := validRawEvent()
	raw.SequenceNum = 0
	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.SequenceNum)
}

func TestParse_NilPayload_DefaultsToEmptyObject(t *testing.T) {
	raw := validRawEvent()
	raw.Payload = nil
	result, err := Parse(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(result.Payload))
}

func TestParse_InvalidPayload_NotJSON(t *testing.T) {
	raw := validRawEvent()
	raw.Payload = json.RawMessage(`not json`)
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidPayload)
}

func TestParse_InvalidPayload_Array(t *testing.T) {
	raw := validRawEvent()
	raw.Payload = json.RawMessage(`[1,2,3]`)
	_, err := Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidPayload)
}

func TestParse_TrimsWhitespace(t *testing.T) {
	raw := validRawEvent()
	raw.IdempotencyKey = "  key-trimmed  "
	raw.ContractID = "  contract-trimmed  "
	raw.TenantID = "  tenant-trimmed  "
	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "key-trimmed", result.IdempotencyKey)
	assert.Equal(t, "contract-trimmed", result.ContractID)
	assert.Equal(t, "tenant-trimmed", result.TenantID)
}

func TestParse_ValidSubscriptionCreated(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(ValidSubscriptionCreated), &raw)
	require.NoError(t, err)

	result, err := Parse(raw)
	require.NoError(t, err)

	assert.Equal(t, "evt-001", result.IdempotencyKey)
	assert.Equal(t, EventContractCreated, result.EventType)
	assert.Equal(t, "contract-123", result.ContractID)
	assert.Equal(t, "tenant-456", result.TenantID)
	assert.Equal(t, int64(1), result.SequenceNum)

	var payload map[string]interface{}
	err = json.Unmarshal(result.Payload, &payload)
	require.NoError(t, err)
	assert.Equal(t, "sub-789", payload["subscriber_id"])
	assert.Equal(t, "plan-basic", payload["plan_id"])
	assert.Equal(t, "9.99", payload["amount"])
	assert.Equal(t, "USD", payload["currency"])
	assert.Equal(t, "month", payload["interval"])
}

func TestParse_ValidSubscriptionAmended(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(ValidSubscriptionAmended), &raw)
	require.NoError(t, err)

	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EventContractAmended, result.EventType)

	var payload map[string]interface{}
	err = json.Unmarshal(result.Payload, &payload)
	require.NoError(t, err)
	assert.Equal(t, "plan-premium", payload["plan_id"])
}

func TestParse_ValidSubscriptionRenewed(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(ValidSubscriptionRenewed), &raw)
	require.NoError(t, err)

	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EventContractRenewed, result.EventType)
}

func TestParse_ValidSubscriptionCancelled(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(ValidSubscriptionCancelled), &raw)
	require.NoError(t, err)

	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EventContractCancelled, result.EventType)

	var payload map[string]interface{}
	err = json.Unmarshal(result.Payload, &payload)
	require.NoError(t, err)
	assert.Equal(t, "user_request", payload["reason"])
}

func TestParse_ValidSubscriptionExpired(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(ValidSubscriptionExpired), &raw)
	require.NoError(t, err)

	result, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EventContractExpired, result.EventType)
}

func TestParse_MissingIdempotencyKey_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(MissingIdempotencyKey), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrMissingIdempotencyKey)
}

func TestParse_MissingEventType_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(MissingEventType), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrMissingEventType)
}

func TestParse_InvalidEventType_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(InvalidEventType), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidEventType)
}

func TestParse_MissingContractID_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(MissingContractID), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrMissingContractID)
}

func TestParse_MissingTenantID_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(MissingTenantID), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrMissingTenantID)
}

func TestParse_MissingOccurredAt_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(MissingOccurredAt), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrMissingOccurredAt)
}

func TestParse_InvalidOccurredAt_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(InvalidOccurredAt), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidOccurredAt)
}

func TestParse_InvalidPayload_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(InvalidPayload), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidPayload)
}

func TestParse_NegativeSequence_FromFixture(t *testing.T) {
	var raw RawEvent
	err := json.Unmarshal([]byte(NegativeSequence), &raw)
	require.NoError(t, err)
	_, err = Parse(raw)
	assert.ErrorIs(t, err, ErrNegativeSequence)
}
