package outbox

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shardedMemRepo extends memoryRepository with shard-aware queries for testing.
type shardedMemRepo struct {
	memoryRepository
}

func newShardedMemRepo() *shardedMemRepo {
	return &shardedMemRepo{
		memoryRepository: *newMemoryRepository(),
	}
}

func (r *shardedMemRepo) GetPendingEventsForShards(shards []int, limit int) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	shardSet := make(map[int]struct{}, len(shards))
	for _, s := range shards {
		shardSet[s] = struct{}{}
	}
	var pending []*Event
	lastID, hasProgress := r.progress["default"]
	for _, event := range r.events {
		if event.Status != StatusPending {
			continue
		}
		if event.NextRetryAt != nil && event.NextRetryAt.After(now) {
			continue
		}
		if _, ok := shardSet[event.Partition]; !ok {
			continue
		}
		if hasProgress && event.ID.String() <= lastID.String() {
			continue
		}
		pending = append(pending, event)
		if len(pending) >= limit {
			break
		}
	}
	return pending, nil
}

// --- Sharded Dispatcher Tests ---

func TestNewShardedDispatcher_Validation(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, _, _ := sqlmock.New()
	defer db.Close()

	tests := []struct {
		name    string
		config  DispatcherConfig
		wantErr string
	}{
		{
			name:    "zero shard count",
			config:  DispatcherConfig{ShardCount: 0, OwnedShards: []int{0}},
			wantErr: "shard count must be positive",
		},
		{
			name:    "no owned shards",
			config:  DispatcherConfig{ShardCount: 4, OwnedShards: nil},
			wantErr: "must own at least one shard",
		},
		{
			name:    "shard out of range",
			config:  DispatcherConfig{ShardCount: 4, OwnedShards: []int{5}},
			wantErr: "out of range",
		},
		{
			name:    "nil database",
			config:  DispatcherConfig{ShardCount: 4, OwnedShards: []int{0}},
			wantErr: "database connection required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dbArg *sql.DB
			if tt.name != "nil database" {
				dbArg = db
			}
			_, err := NewShardedDispatcher(repo, publisher, dbArg, tt.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewShardedDispatcher_Success(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, _, _ := sqlmock.New()
	defer db.Close()

	d, err := NewShardedDispatcher(repo, publisher, db, DispatcherConfig{
		ShardCount:  8,
		OwnedShards: []int{0, 3, 7},
	})
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.False(t, d.IsRunning())
}

func TestShardedDispatcherLifecycle(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Advisory lock mock for Start()
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	// Advisory unlock mock for Stop()
	mock.ExpectQuery(`SELECT pg_advisory_unlock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      time.Hour,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)

	require.NoError(t, d.Start())
	assert.True(t, d.IsRunning())

	// Idempotent start
	require.NoError(t, d.Start())

	require.NoError(t, d.Stop())
	assert.False(t, d.IsRunning())

	// Idempotent stop
	require.NoError(t, d.Stop())
}

func TestShardedDispatcher_AdvisoryLockFailure(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Advisory lock fails
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(5).
		WillReturnError(errors.New("connection lost"))

	cfg := DispatcherConfig{
		ShardCount:  8,
		OwnedShards: []int{5},
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)

	err = d.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire advisory lock")
}

func TestShardedDispatcher_PartitionFiltering(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Advisory lock
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	// Create events in different partitions
	for i := 0; i < 10; i++ {
		event := &Event{
			ID:         uuid.New(),
			EventType:  "test.event",
			EventData:  json.RawMessage(`{"type":"test"}`),
			Partition:  i % 4,
			Status:     StatusPending,
			OccurredAt: time.Now(),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Version:    1,
		}
		require.NoError(t, repo.Store(event))
	}

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Only events in partition 0 should be published.
	published := publisher.GetPublishedEvents()
	for _, ev := range published {
		assert.Equal(t, 0, ev.Partition, "only partition 0 events should be published")
	}
}

func TestShardedDispatcher_PublishesCorrectShardEvents(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(2).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	ring := NewConsistentHashRing(4, 150)

	// Create events for "tenant-A" which maps to a specific partition
	for i := 0; i < 5; i++ {			event := &Event{
				ID:         uuid.New(),
				EventType:  "tenant.event",
				EventData:  json.RawMessage(`{"type":"tenant"}`),
				TenantID:   "tenant-A",
				Partition:  ring.GetPartition("tenant-A"),
			Status:     StatusPending,
			OccurredAt: time.Now(),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Version:    1,
		}
		require.NoError(t, repo.Store(event))
	}

	// Determine which shard tenant-A maps to
	tenantShard := ring.GetPartition("tenant-A")

	// If tenant-A doesn't map to shard 2, create events that do
	if tenantShard != 2 {
		// Find a tenant that maps to shard 2
		for i := 0; i < 100; i++ {
			candidate := "shard2-tenant-" + string(rune('A'+i))
			if ring.GetPartition(candidate) == 2 {
				for j := 0; j < 5; j++ {
					event := &Event{
						ID:        uuid.New(),
						EventType: "shard2.event",
						EventData: json.RawMessage(`{"type":"shard2"}`),
						TenantID:  candidate,
						Partition: 2,
						Status:    StatusPending,
						OccurredAt: time.Now(),
						CreatedAt:  time.Now(),
						UpdatedAt:  time.Now(),
						Version:   1,
					}
					require.NoError(t, repo.Store(event))
				}
				break
			}
		}
	}

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{2},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          20,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	time.Sleep(300 * time.Millisecond)

	published := publisher.GetPublishedEvents()
	// All published events should be in the shard we own (2).
	for _, ev := range published {
		assert.Equal(t, 2, ev.Partition)
	}
}

func TestShardedDispatcher_FailureIsolation(t *testing.T) {
	repo := newShardedMemRepo()
	failPub := &failPublisher{}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event := &Event{
		ID:         uuid.New(),
		EventType:  "test",
		EventData:  json.RawMessage(`{"type":"test"}`),
		Partition:  0,
		Status:     StatusPending,
		OccurredAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	}
	require.NoError(t, repo.Store(event))

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  200 * time.Millisecond,
		MaxRetries:         1,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, failPub, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	// The failing publisher should not crash the dispatcher.
	// After MaxRetries, the event is dead-lettered.
	require.Eventually(t, func() bool {
		ev, _ := repo.GetByID(event.ID)
		return ev != nil && ev.Status == StatusFailed
	}, 2*time.Second, 20*time.Millisecond)
}

func TestShardedDispatcher_CleanupCompletedEvents(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event, err := NewEvent("cleanup.me", map[string]string{"k": "v"}, nil, nil)
	require.NoError(t, err)
	event.Status = StatusCompleted
	event.UpdatedAt = time.Now().Add(-time.Hour)
	event.Partition = 0
	require.NoError(t, repo.Store(event))

	cfg := DispatcherConfig{
		ShardCount:         4,
		OwnedShards:        []int{0},
		PollInterval:       time.Hour,
		CleanupInterval:    20 * time.Millisecond,
		CompletedEventTTL:  time.Millisecond,
		HeartbeatInterval:  time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		_, getErr := repo.GetByID(event.ID)
		return getErr != nil
	}, 2*time.Second, 20*time.Millisecond)
}

func TestShardedDispatcher_HighWaterMarkSkipsDeliveredEvents(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event := &Event{
		ID:         uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		EventType:  "already.delivered",
		EventData:  json.RawMessage(`{"type":"already.delivered"}`),
		Partition:  0,
		OccurredAt: time.Now(),
		Status:     StatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	}
	require.NoError(t, repo.Store(event))
	// Mark as already delivered
	repo.progress["default"] = event.ID

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          5,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, publisher.GetPublishedEvents())
}

func TestShardedDispatcher_PermanentErrorDeadLetters(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event, err := NewEvent("perm.fail", map[string]string{"x": "y"}, nil, nil)
	require.NoError(t, err)
	event.Partition = 0
	require.NoError(t, repo.Store(event))
	publisher.SetPublishError(event.ID, &PermanentPublishError{Reason: "missing key"})

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		stored, getErr := repo.GetByID(event.ID)
		return getErr == nil && stored.Status == StatusFailed
	}, 2*time.Second, 20*time.Millisecond)
}

func TestShardedDispatcher_FallbackFilteringWithoutShardedRepo(t *testing.T) {
	// Use the basic memRepo (no ShardedRepository implementation) to test fallback.
	repo := newMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	for i := 0; i < 5; i++ {
		partition := i % 4
		e := &Event{
			ID:         uuid.New(),
			EventType:  "test",
			EventData:  json.RawMessage(`{"type":"test"}`),
			Partition:  partition,
			OccurredAt: time.Now(),
		}
		repo.Store(e)
	}

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          20,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval: time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	time.Sleep(300 * time.Millisecond)

	published := publisher.GetPublishedEvents()
	for _, ev := range published {
		assert.Equal(t, 0, ev.Partition, "fallback should still filter by partition")
	}
}

func TestShardedDispatcher_TransientRetryBackoff(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event, err := NewEvent("retry.me", map[string]string{"k": "v"}, nil, nil)
	require.NoError(t, err)
	event.Partition = 0
	require.NoError(t, repo.Store(event))
	publisher.SetPublishError(event.ID, errors.New("transient"))

	cfg := DispatcherConfig{
		ShardCount:         4,
		OwnedShards:        []int{0},
		PollInterval:       20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  200 * time.Millisecond,
		MaxRetries:         1,
		RetryBackoffFactor: 2.0,
		HeartbeatInterval:  time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	require.Eventually(t, func() bool {
		stored, getErr := repo.GetByID(event.ID)
		return getErr == nil && stored.Status == StatusFailed
	}, 2*time.Second, 20*time.Millisecond)
}

func TestShardedDispatcher_VerifyLockHealth_NilConn(t *testing.T) {
	sd := &shardedDispatcher{}
	sd.verifyLockHealth()
}

func TestShardedDispatcher_EmptyOwnedShardsDrainSkips(t *testing.T) {
	repo := newShardedMemRepo()
	publisher := NewMockPublisher()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event := &Event{
		ID:         uuid.New(),
		EventType:  "test",
		EventData:  json.RawMessage(`{"type":"test"}`),
		Partition:  1,
		Status:     StatusPending,
		OccurredAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	}
	require.NoError(t, repo.Store(event))

	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  200 * time.Millisecond,
		HeartbeatInterval:  time.Hour,
	}

	d, err := NewShardedDispatcher(repo, publisher, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())

	time.Sleep(200 * time.Millisecond)

	published := publisher.GetPublishedEvents()
	for _, ev := range published {
		assert.Equal(t, 0, ev.Partition, "only partition 0 events should be published")
	}

	d.Stop()
}

func TestShardedDispatcher_ContextTimeoutDuringPublish(t *testing.T) {
	repo := newShardedMemRepo()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(0).
		WillReturnRows(sqlmock.NewRows([]string{"result"}).AddRow(true))

	event := &Event{
		ID:         uuid.New(),
		EventType:  "slow.event",
		EventData:  json.RawMessage(`{"type":"slow"}`),
		Partition:  0,
		Status:     StatusPending,
		OccurredAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	}
	require.NoError(t, repo.Store(event))

	slowPub := &slowFailPublisher{}
	cfg := DispatcherConfig{
		ShardCount:        4,
		OwnedShards:       []int{0},
		PollInterval:      20 * time.Millisecond,
		BatchSize:          10,
		ProcessingTimeout:  10 * time.Millisecond,
		HeartbeatInterval:  time.Hour,
	}

	d, err := NewShardedDispatcher(repo, slowPub, db, cfg)
	require.NoError(t, err)
	require.NoError(t, d.Start())
	defer d.Stop()

	time.Sleep(200 * time.Millisecond)
}

// strPtr is a test helper that returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
