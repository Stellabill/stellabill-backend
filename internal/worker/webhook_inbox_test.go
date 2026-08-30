package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/testutil"
)

type MockOutboxRepo struct {
	mock.Mock
}

func (m *MockOutboxRepo) Store(ctx context.Context, event *outbox.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}
func (m *MockOutboxRepo) GetPendingEvents(limit int) ([]*outbox.Event, error)         { return nil, nil }
func (m *MockOutboxRepo) GetByID(id uuid.UUID) (*outbox.Event, error)                 { return nil, nil }
func (m *MockOutboxRepo) UpdateStatus(id uuid.UUID, status outbox.Status, errorMessage *string) error { return nil }
func (m *MockOutboxRepo) MarkAsProcessing(id uuid.UUID) error                         { return nil }
func (m *MockOutboxRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error { return nil }
func (m *MockOutboxRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error)    { return 0, nil }
func (m *MockOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error)   { return nil, nil }
func (m *MockOutboxRepo) RequeueEvent(id uuid.UUID) error                             { return nil }
func (m *MockOutboxRepo) EnsurePublisherProgressTable() error                         { return nil }
func (m *MockOutboxRepo) GetPublisherProgress(publisher string) (*uuid.UUID, error)   { return nil, nil }
func (m *MockOutboxRepo) GetPendingEventsForPublisher(publisher string, limit int) ([]*outbox.Event, error) {
	return nil, nil
}
func (m *MockOutboxRepo) MarkPublished(publisher string, event *outbox.Event, publishers []string) error {
	return nil
}


func TestWorker_PreservesOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	ctx := context.Background()

	container, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err, "Failed to start postgres container")
	t.Cleanup(func() {
		_ = container.Teardown(ctx)
	})

	err = testutil.ApplyMigrations(ctx, container.DSN)
	require.NoError(t, err, "Failed to apply migrations")

	pool, err := testutil.NewPoolFromDSN(ctx, container.DSN)
	require.NoError(t, err, "Failed to connect to test database")
	t.Cleanup(func() {
		pool.Close()
	})

	_, err = pool.Exec(ctx, "TRUNCATE TABLE webhook_inbox")
	require.NoError(t, err)

	mockOutbox := new(MockOutboxRepo)
	mockOutbox.On("Store", mock.Anything, mock.Anything).Return(nil)

	w := &WebhookWorker{
		DB:         pool,
		OutboxRepo: mockOutbox,
	}

	sourceID := "source_A"
	now := time.Now().UTC()

	payload := []byte(`{"event_type": "subscription.created", "data": {"subscription_id": "sub_123"}}`)

	type webhook struct {
		ID        string
		CreatedAt time.Time
	}
	
	webhooks := []webhook{
		{ID: uuid.NewString(), CreatedAt: now.Add(-3 * time.Second)},
		{ID: uuid.NewString(), CreatedAt: now.Add(-2 * time.Second)},
		{ID: uuid.NewString(), CreatedAt: now.Add(-1 * time.Second)},
	}

	for i, wh := range webhooks {
		msgID := fmt.Sprintf("msg_test_%d", i)
		_, err := pool.Exec(ctx, `
			INSERT INTO webhook_inbox (id, provider, provider_msg_id, source_id, payload, status, created_at, updated_at)
			VALUES ($1, 'stripe', $2, $3, $4, 'pending', $5, $5)`,
			wh.ID, msgID, sourceID, payload, wh.CreatedAt)
		require.NoError(t, err)
	}

	w.processBatch(ctx)

	var oldestStatus string
	err = pool.QueryRow(ctx, "SELECT status FROM webhook_inbox WHERE id = $1", webhooks[0].ID).Scan(&oldestStatus)
	require.NoError(t, err)
	assert.Equal(t, "processed", oldestStatus, "The oldest webhook should be processed")

	for i := 1; i < len(webhooks); i++ {
		var status string
		err = pool.QueryRow(ctx, "SELECT status FROM webhook_inbox WHERE id = $1", webhooks[i].ID).Scan(&status)
		require.NoError(t, err)
		assert.Equal(t, "pending", status, fmt.Sprintf("Webhook %d must remain pending until the older one is processed", i))
	}

	mockOutbox.AssertNumberOfCalls(t, "Store", 1)

	w.processBatch(ctx)
	
	var secondStatus string
	err = pool.QueryRow(ctx, "SELECT status FROM webhook_inbox WHERE id = $1", webhooks[1].ID).Scan(&secondStatus)
	require.NoError(t, err)
	assert.Equal(t, "processed", secondStatus, "The second oldest webhook should now be processed")
	
	mockOutbox.AssertNumberOfCalls(t, "Store", 2)
}