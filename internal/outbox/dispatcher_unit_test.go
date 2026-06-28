package outbox

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryRepository struct {
	mu     sync.Mutex
	events map[uuid.UUID]*Event
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{events: make(map[uuid.UUID]*Event)}
}

func (m *memoryRepository) Store(event *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *event
	m.events[event.ID] = &copy
	return nil
}

func (m *memoryRepository) GetPendingEvents(limit int) ([]*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var pending []*Event
	for _, event := range m.events {
		if event.Status != StatusPending {
			continue
		}
		if event.NextRetryAt != nil && event.NextRetryAt.After(now) {
			continue
		}
		pending = append(pending, event)
		if len(pending) >= limit {
			break
		}
	}
	return pending, nil
}

func (m *memoryRepository) GetByID(id uuid.UUID) (*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	event, ok := m.events[id]
	if !ok {
		return nil, errors.New("not found")
	}
	copy := *event
	return &copy, nil
}

func (m *memoryRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event, ok := m.events[id]
	if !ok {
		return errors.New("not found")
	}
	event.Status = status
	event.ErrorMessage = errorMessage
	event.UpdatedAt = time.Now()
	return nil
}

func (m *memoryRepository) MarkAsProcessing(id uuid.UUID) error {
	return m.UpdateStatus(id, StatusProcessing, nil)
}

func (m *memoryRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event, ok := m.events[id]
	if !ok {
		return errors.New("not found")
	}
	event.RetryCount++
	event.NextRetryAt = &nextRetryAt
	event.ErrorMessage = errorMessage
	event.Status = StatusPending
	return nil
}

func (m *memoryRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var deleted int64
	for id, event := range m.events {
		if event.Status == StatusCompleted && event.UpdatedAt.Before(olderThan) {
			delete(m.events, id)
			deleted++
		}
	}
	return deleted, nil
}

func (m *memoryRepository) ListDeadLetteredEvents(limit int) ([]*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var failed []*Event
	for _, event := range m.events {
		if event.Status == StatusFailed {
			copy := *event
			failed = append(failed, &copy)
			if len(failed) >= limit {
				break
			}
		}
	}
	return failed, nil
}

func (m *memoryRepository) RequeueEvent(id uuid.UUID) error {
	return m.UpdateStatus(id, StatusPending, nil)
}

func TestDefaultDispatcherConfig(t *testing.T) {
	cfg := DefaultDispatcherConfig()
	assert.Equal(t, 10, cfg.BatchSize)
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestDispatcherLifecycle(t *testing.T) {
	repo := newMemoryRepository()
	publisher := NewMockPublisher()
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = time.Hour

	d := NewDispatcher(repo, publisher, cfg)
	assert.False(t, d.IsRunning())

	require.NoError(t, d.Start())
	assert.True(t, d.IsRunning())
	require.NoError(t, d.Start()) // idempotent

	require.NoError(t, d.Stop())
	assert.False(t, d.IsRunning())
	require.NoError(t, d.Stop()) // idempotent
}

func TestDispatcherPublishesPendingEvent(t *testing.T) {
	repo := newMemoryRepository()
	publisher := NewMockPublisher()
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 20 * time.Millisecond
	cfg.BatchSize = 5

	event, err := NewEvent("user.created", map[string]string{"id": "1"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event))

	d := NewDispatcher(repo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		return len(publisher.GetPublishedEvents()) == 1
	}, 2*time.Second, 20*time.Millisecond)

	stored, err := repo.GetByID(event.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, stored.Status)
}

func TestDispatcherPermanentErrorDeadLetters(t *testing.T) {
	repo := newMemoryRepository()
	publisher := NewMockPublisher()
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 20 * time.Millisecond

	event, err := NewEvent("payment.processed", map[string]string{"x": "y"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event))
	publisher.SetPublishError(event.ID, &PermanentPublishError{Reason: "missing key"})

	d := NewDispatcher(repo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		stored, getErr := repo.GetByID(event.ID)
		return getErr == nil && stored.Status == StatusFailed
	}, 2*time.Second, 20*time.Millisecond)
}

func TestDispatcherRetriesTransientErrors(t *testing.T) {
	repo := newMemoryRepository()
	publisher := NewMockPublisher()
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 20 * time.Millisecond
	cfg.MaxRetries = 2

	event, err := NewEvent("retry.me", map[string]string{"k": "v"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event))
	publisher.SetPublishError(event.ID, errors.New("transient"))

	d := NewDispatcher(repo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		stored, getErr := repo.GetByID(event.ID)
		return getErr == nil && stored.RetryCount >= 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestDispatcherCleanupCompletedEvents(t *testing.T) {
	repo := newMemoryRepository()
	publisher := NewMockPublisher()
	cfg := DefaultDispatcherConfig()
	cfg.CleanupInterval = 20 * time.Millisecond
	cfg.CompletedEventTTL = time.Millisecond

	event, err := NewEvent("cleanup.me", map[string]string{"k": "v"}, nil, nil)
	require.NoError(t, err)
	event.Status = StatusCompleted
	event.UpdatedAt = time.Now().Add(-time.Hour)
	require.NoError(t, repo.Store(event))

	d := NewDispatcher(repo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		_, getErr := repo.GetByID(event.ID)
		return getErr != nil
	}, 2*time.Second, 20*time.Millisecond)
}

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{msg: "timed out"}
	assert.Equal(t, "timed out", err.Error())
}

func TestPermanentPublishErrorMethods(t *testing.T) {
	root := errors.New("root cause")
	err := &PermanentPublishError{Reason: "missing key", Err: root}
	assert.Contains(t, err.Error(), "missing key")
	assert.Equal(t, root, err.Unwrap())
	assert.True(t, IsPermanentPublishError(err))
	assert.False(t, IsPermanentPublishError(errors.New("other")))
}

func TestPrepareEncryptedEventData(t *testing.T) {
	pubJWK, _ := generateTestRSAJWK(t, "store-key")
	repo := newMemorySubscriberKeyRepo()
	require.NoError(t, repo.Create(&SubscriberKey{
		SubscriberID: "sub-store",
		KeyID:        "store-key",
		JWK:          pubJWK,
		Status:       SubscriberKeyActive,
	}))

	sensitive := NewSensitiveEventRegistry([]string{"webhook.received"})
	raw, err := PrepareEncryptedEventData(
		"webhook.received",
		map[string]string{"token": "secret"},
		"sub-store",
		repo,
		NewJWEEncryptor(),
		sensitive,
	)
	require.NoError(t, err)

	var envelope EventData
	require.NoError(t, json.Unmarshal(raw, &envelope))
	assert.True(t, envelope.Encrypted)
	assert.NotEmpty(t, envelope.JWE)
	assert.Equal(t, "store-key", envelope.KeyID)

	plain, err := PrepareEncryptedEventData(
		"user.created",
		map[string]string{"id": "1"},
		"sub-store",
		repo,
		NewJWEEncryptor(),
		sensitive,
	)
	require.NoError(t, err)
	var plainEnvelope EventData
	require.NoError(t, json.Unmarshal(plain, &plainEnvelope))
	assert.False(t, plainEnvelope.Encrypted)

	_, err = PrepareEncryptedEventData("webhook.received", nil, "", repo, NewJWEEncryptor(), sensitive)
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
}

func TestResolveSubscriberIDFromEventData(t *testing.T) {
	subscriberID := "from-data"
	raw, err := json.Marshal(EventData{SubscriberID: subscriberID})
	require.NoError(t, err)
	event := &Event{EventData: raw}
	assert.Equal(t, subscriberID, ResolveSubscriberID(event))
}
