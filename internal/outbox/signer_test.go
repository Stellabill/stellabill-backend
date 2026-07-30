package outbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSigningKeyProvider returns a fixed key for deterministic tests.
type mockSigningKeyProvider struct {
	key jwk.Key
	kid string
}

func newMockSigningKeyProvider(t *testing.T) *mockSigningKeyProvider {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	key, err := jwk.FromRaw(priv)
	require.NoError(t, err)

	kid := "test-kid-001"
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))

	return &mockSigningKeyProvider{key: key, kid: kid}
}

func (m *mockSigningKeyProvider) SigningKey(_ context.Context) (jwk.Key, string, error) {
	return m.key, m.kid, nil
}

// mockPublisher captures events for verification.
type mockSignerPublisher struct {
	events []*Event
}

func (m *mockSignerPublisher) Publish(_ context.Context, event *Event) error {
	m.events = append(m.events, event)
	return nil
}

func TestNewEd25519Signer(t *testing.T) {
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)
	require.NotNil(t, signer)

	_, ok := signer.(*Ed25519Signer)
	assert.True(t, ok, "expected *Ed25519Signer")
}

func TestEd25519Signer_SignsPayload(t *testing.T) {
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)

	event := &Event{
		ID:            uuid.New(),
		EventType:     "subscription.updated",
		EventData:     json.RawMessage(`{"type":"subscription.updated","data":{"plan":"pro"},"timestamp":"2026-07-28T12:00:00Z","id":"evt-001"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-123"),
		AggregateType: strPtr("subscription"),
		Version:       2,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)
	require.Len(t, inner.events, 1, "expected one published event")

	published := inner.events[0]

	// Unmarshal to check signed envelope
	var signedEnvelope SignedEventData
	err = json.Unmarshal(published.EventData, &signedEnvelope)
	require.NoError(t, err)

	assert.True(t, signedEnvelope.Signed, "expected signed flag")
	assert.NotEmpty(t, signedEnvelope.Signature, "expected signature")
	assert.Equal(t, "test-kid-001", signedEnvelope.SigningKeyID, "expected signing key ID")
}

func TestEd25519Signer_VerifySignature(t *testing.T) {
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)

	event := &Event{
		ID:            uuid.New(),
		EventType:     "payment.processed",
		EventData:     json.RawMessage(`{"type":"payment.processed","data":{"amount":5000,"currency":"USD"},"timestamp":"2026-07-28T12:00:00Z","id":"evt-002"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-456"),
		AggregateType: strPtr("subscription"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)

	// Verify using the public key
	pubKey, err := provider.key.PublicKey()
	require.NoError(t, err)

	payload, err := VerifyOutboxSignature(inner.events[0].EventData, pubKey)
	require.NoError(t, err, "signature should verify")

	// Verify the payload matches
	var parsed map[string]interface{}
	err = json.Unmarshal(payload, &parsed)
	require.NoError(t, err)
	assert.Equal(t, event.ID.String(), parsed["id"])
	assert.Equal(t, event.EventType, parsed["type"])
}

func TestEd25519Signer_TamperedSignature(t *testing.T) {
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)

	event := &Event{
		ID:            uuid.New(),
		EventType:     "test.event",
		EventData:     json.RawMessage(`{"type":"test.event","data":{"key":"value"},"timestamp":"2026-07-28T12:00:00Z","id":"evt-003"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-789"),
		AggregateType: strPtr("subscription"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)

	// Tamper with the event data
	published := inner.events[0]
	var tampered SignedEventData
	err = json.Unmarshal(published.EventData, &tampered)
	require.NoError(t, err)

	tampered.Data = map[string]interface{}{"key": "tampered-value"}
	tamperedJSON, _ := json.Marshal(tampered)

	// Verification should fail
	pubKey, err := provider.key.PublicKey()
	require.NoError(t, err)

	_, err = VerifyOutboxSignature(tamperedJSON, pubKey)
	assert.Error(t, err, "expected verification to fail on tampered data")
}

func TestEd25519Signer_UnsingedEventFails(t *testing.T) {
	provider := newMockSigningKeyProvider(t)
	pubKey, err := provider.key.PublicKey()
	require.NoError(t, err)

	// Unsigned event data
	unsignedData := json.RawMessage(`{"type":"test.event","data":{},"timestamp":"2026-07-28T12:00:00Z","id":"evt-004"}`)

	_, err = VerifyOutboxSignature(unsignedData, pubKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not signed")
}

func TestEd25519Signer_WrongKeyFails(t *testing.T) {
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)

	event := &Event{
		ID:            uuid.New(),
		EventType:     "test.event",
		EventData:     json.RawMessage(`{"type":"test.event","data":{"msg":"hello"},"timestamp":"2026-07-28T12:00:00Z","id":"evt-005"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-000"),
		AggregateType: strPtr("subscription"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)

	// Generate a different key for verification
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	wrongPubJWK, err := jwk.FromRaw(wrongPriv.Public())
	require.NoError(t, err)

	_, err = VerifyOutboxSignature(inner.events[0].EventData, wrongPubJWK)
	assert.Error(t, err, "expected verification to fail with wrong key")
}

func TestPublicJWKFromSigningKey(t *testing.T) {
	provider := newMockSigningKeyProvider(t)

	pubJWK, err := PublicJWKFromSigningKey(provider.key)
	require.NoError(t, err)

	// Verify it's a public key (shouldn't have private key material)
	var rawKey interface{}
	err = pubJWK.Raw(&rawKey)
	require.NoError(t, err)

	_, ok := rawKey.(ed25519.PublicKey)
	assert.True(t, ok, "expected ed25519.PublicKey")

	// Verify kid is preserved
	kid, ok := pubJWK.KeyID()
	assert.True(t, ok)
	assert.Equal(t, "test-kid-001", kid)
}

func TestNewEd25519Signer_NilProviderDefaults(t *testing.T) {
	inner := &mockSignerPublisher{}
	signer := NewEd25519Signer(inner, nil)
	require.NotNil(t, signer)

	// Should publish successfully with ephemeral key
	event := &Event{
		ID:            uuid.New(),
		EventType:     "test.event",
		EventData:     json.RawMessage(`{"type":"test.event","data":{},"timestamp":"2026-07-28T12:00:00Z","id":"evt-006"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-111"),
		AggregateType: strPtr("subscription"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)
	require.Len(t, inner.events, 1)
}

func TestEd25519Signer_ProviderError(t *testing.T) {
	errProvider := &errSigningKeyProvider{}
	inner := &mockSignerPublisher{}

	signer := NewEd25519Signer(inner, errProvider)

	event := &Event{
		ID:            uuid.New(),
		EventType:     "test.event",
		EventData:     json.RawMessage(`{"type":"test.event","data":{},"timestamp":"2026-07-28T12:00:00Z","id":"evt-007"}`),
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("sub-222"),
		AggregateType: strPtr("subscription"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signing key unavailable")
}

type errSigningKeyProvider struct{}

func (e *errSigningKeyProvider) SigningKey(_ context.Context) (jwk.Key, string, error) {
	return nil, "", errors.New("key provider unavailable")
}

func TestEd25519Signer_JWSRoundTrip(t *testing.T) {
	// Full round-trip: sign with one key, verify with the corresponding public key
	inner := &mockSignerPublisher{}
	provider := newMockSigningKeyProvider(t)
	signer := NewEd25519Signer(inner, provider)

	originalData := map[string]interface{}{
		"plan": "enterprise",
		"seats": 50,
	}
	eventData, _ := json.Marshal(map[string]interface{}{
		"type":      "plan.changed",
		"data":     originalData,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"id":       "evt-roundtrip",
	})

	event := &Event{
		ID:            uuid.New(),
		EventType:     "plan.changed",
		EventData:     eventData,
		OccurredAt:    time.Now(),
		AggregateID:   strPtr("plan-abc"),
		AggregateType: strPtr("plan"),
		Version:       1,
	}

	err := signer.Publish(context.Background(), event)
	require.NoError(t, err)

	// Get public key
	pubKey, err := provider.key.PublicKey()
	require.NoError(t, err)

	// Verify
	verifiedPayload, err := VerifyOutboxSignature(inner.events[0].EventData, pubKey)
	require.NoError(t, err)

	// Check payload contents
	var payload map[string]interface{}
	err = json.Unmarshal(verifiedPayload, &payload)
	require.NoError(t, err)
	assert.Equal(t, "plan.changed", payload["type"])
}

// strPtr is a helper for creating *string values.
func strPtr(s string) *string {
	return &s
}
