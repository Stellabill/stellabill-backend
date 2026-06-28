package outbox

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memorySubscriberKeyRepo struct {
	keys map[string][]*SubscriberKey
	byID map[uuid.UUID]*SubscriberKey
}

func newMemorySubscriberKeyRepo() *memorySubscriberKeyRepo {
	return &memorySubscriberKeyRepo{
		keys: make(map[string][]*SubscriberKey),
		byID: make(map[uuid.UUID]*SubscriberKey),
	}
}

func (m *memorySubscriberKeyRepo) Create(key *SubscriberKey) error {
	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	m.keys[key.SubscriberID] = append(m.keys[key.SubscriberID], key)
	m.byID[key.ID] = key
	return nil
}

func (m *memorySubscriberKeyRepo) GetByID(id uuid.UUID) (*SubscriberKey, error) {
	if key, ok := m.byID[id]; ok {
		return key, nil
	}
	return nil, ErrMissingSubscriberKey
}

func (m *memorySubscriberKeyRepo) ListBySubscriber(subscriberID string) ([]*SubscriberKey, error) {
	return m.keys[subscriberID], nil
}

func (m *memorySubscriberKeyRepo) GetActiveKey(subscriberID string) (*SubscriberKey, error) {
	keys := m.keys[subscriberID]
	for i := len(keys) - 1; i >= 0; i-- {
		k := keys[i]
		if k.Status != SubscriberKeyActive {
			continue
		}
		if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
			continue
		}
		return k, nil
	}
	return nil, ErrMissingSubscriberKey
}

func (m *memorySubscriberKeyRepo) UpdateStatus(id uuid.UUID, status SubscriberKeyStatus) error {
	key, ok := m.byID[id]
	if !ok {
		return ErrMissingSubscriberKey
	}
	key.Status = status
	return nil
}

func generateTestRSAJWK(t *testing.T, kid string) (json.RawMessage, json.RawMessage) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pubJWK, err := jwk.FromRaw(&privateKey.PublicKey)
	require.NoError(t, err)
	require.NoError(t, pubJWK.Set(jwk.KeyIDKey, kid))

	privJWK, err := jwk.FromRaw(privateKey)
	require.NoError(t, err)

	pubBytes, err := json.Marshal(pubJWK)
	require.NoError(t, err)
	privBytes, err := json.Marshal(privJWK)
	require.NoError(t, err)
	return pubBytes, privBytes
}

func TestJWEEncryptorRoundTrip(t *testing.T) {
	pubJWK, privJWK := generateTestRSAJWK(t, "test-key-1")
	encryptor := NewJWEEncryptor()

	payload := map[string]string{"secret": "billing-data"}
	compact, err := encryptor.Encrypt(payload, pubJWK)
	require.NoError(t, err)
	assert.NotEmpty(t, compact)

	plaintext, err := DecryptForTest(compact, privJWK)
	require.NoError(t, err)

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(plaintext, &decoded))
	assert.Equal(t, "billing-data", decoded["secret"])
}

func TestSensitiveEventRegistry(t *testing.T) {
	reg := NewSensitiveEventRegistry([]string{"webhook.received"})
	assert.True(t, reg.IsSensitive("webhook.received"))
	assert.False(t, reg.IsSensitive("user.created"))
}

func TestResolveSubscriberID(t *testing.T) {
	subscriberID := "sub-42"
	aggregateType := "subscriber"
	eventData, err := json.Marshal(EventData{
		Type:         "webhook.received",
		SubscriberID: subscriberID,
	})
	require.NoError(t, err)
	event := &Event{
		EventData:     eventData,
		AggregateID:   &subscriberID,
		AggregateType: &aggregateType,
	}
	assert.Equal(t, "sub-42", ResolveSubscriberID(event))
}

func TestJWEPublisherEncryptsSensitiveEvents(t *testing.T) {
	pubJWK, privJWK := generateTestRSAJWK(t, "active-key")
	repo := newMemorySubscriberKeyRepo()
	require.NoError(t, repo.Create(&SubscriberKey{
		SubscriberID: "sub-1",
		KeyID:        "active-key",
		JWK:          pubJWK,
		Status:       SubscriberKeyActive,
	}))

	var receivedContentType string
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedBody, _ = ioReadAll(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDefaultHTTPClient(5*time.Second, "")
	require.NoError(t, err)
	inner := NewHTTPPublisher(server.URL, client)
	publisher := NewJWEPublisher(inner, repo, NewJWEEncryptor(), NewSensitiveEventRegistry(nil))

	subscriberID := "sub-1"
	aggregateType := "subscriber"
	eventData, err := json.Marshal(EventData{
		Type:         "webhook.received",
		Data:         map[string]string{"amount": "100"},
		ID:           "evt-1",
		SubscriberID: subscriberID,
	})
	require.NoError(t, err)

	event := &Event{
		EventType:     "webhook.received",
		EventData:     eventData,
		AggregateID:   &subscriberID,
		AggregateType: &aggregateType,
	}

	err = publisher.Publish(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, "application/jose+json", receivedContentType)

	plaintext, err := DecryptForTest(string(receivedBody), privJWK)
	require.NoError(t, err)
	assert.Contains(t, string(plaintext), "webhook.received")
}

func TestJWEPublisherMissingKeyRoutesToPermanentError(t *testing.T) {
	inner := NewConsolePublisher()
	publisher := NewJWEPublisher(inner, newMemorySubscriberKeyRepo(), NewJWEEncryptor(), NewSensitiveEventRegistry(nil))

	subscriberID := "missing-sub"
	aggregateType := "subscriber"
	eventData, _ := json.Marshal(EventData{Type: "payment.processed", Data: map[string]string{"x": "y"}})
	event := &Event{
		EventType:     "payment.processed",
		EventData:     eventData,
		AggregateID:   &subscriberID,
		AggregateType: &aggregateType,
	}

	err := publisher.Publish(context.Background(), event)
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
}

func TestJWEPublisherKeyRotationMidBatch(t *testing.T) {
	oldPub, _ := generateTestRSAJWK(t, "old-key")
	newPub, newPriv := generateTestRSAJWK(t, "new-key")
	repo := newMemorySubscriberKeyRepo()
	require.NoError(t, repo.Create(&SubscriberKey{
		SubscriberID: "sub-rotate",
		KeyID:        "old-key",
		JWK:          oldPub,
		Status:       SubscriberKeyRevoked,
	}))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, repo.Create(&SubscriberKey{
		SubscriberID: "sub-rotate",
		KeyID:        "new-key",
		JWK:          newPub,
		Status:       SubscriberKeyActive,
	}))

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = ioReadAll(r)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDefaultHTTPClient(5*time.Second, "")
	require.NoError(t, err)
	publisher := NewJWEPublisher(
		NewHTTPPublisher(server.URL, client),
		repo,
		NewJWEEncryptor(),
		NewSensitiveEventRegistry(nil),
	)

	subscriberID := "sub-rotate"
	aggregateType := "subscriber"
	eventData, _ := json.Marshal(EventData{Type: "webhook.received", Data: map[string]string{"v": "1"}})
	event := &Event{
		EventType:     "webhook.received",
		EventData:     eventData,
		AggregateID:   &subscriberID,
		AggregateType: &aggregateType,
	}

	require.NoError(t, publisher.Publish(context.Background(), event))
	_, err = DecryptForTest(string(receivedBody), newPriv)
	require.NoError(t, err)
}

func TestJWEPublisherExpiredKeyIsSkipped(t *testing.T) {
	pubJWK, _ := generateTestRSAJWK(t, "expired-key")
	repo := newMemorySubscriberKeyRepo()
	expired := time.Now().Add(-time.Hour)
	require.NoError(t, repo.Create(&SubscriberKey{
		SubscriberID: "sub-expired",
		KeyID:        "expired-key",
		JWK:          pubJWK,
		Status:       SubscriberKeyActive,
		ExpiresAt:    &expired,
	}))

	publisher := NewJWEPublisher(NewConsolePublisher(), repo, NewJWEEncryptor(), NewSensitiveEventRegistry(nil))
	subscriberID := "sub-expired"
	aggregateType := "subscriber"
	eventData, _ := json.Marshal(EventData{Type: "webhook.received"})
	event := &Event{
		EventType:     "webhook.received",
		EventData:     eventData,
		AggregateID:   &subscriberID,
		AggregateType: &aggregateType,
	}

	err := publisher.Publish(context.Background(), event)
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, r.ContentLength)
	if r.ContentLength > 0 {
		buf = make([]byte, r.ContentLength)
		_, err := r.Body.Read(buf)
		return buf, err
	}
	return buf, nil
}
