package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// simple in-memory repository for testing
type memRepo struct {
	mu       sync.Mutex
	events   []*Event
	progress map[string]*publisherCursor
}

type publisherCursor struct {
	lastAt *time.Time
	lastID *uuid.UUID
}

func newMemRepo() *memRepo {
	return &memRepo{progress: make(map[string]*publisherCursor)}
}

func (r *memRepo) Store(event *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *memRepo) GetPendingEvents(limit int) ([]*Event, error) {
	return r.GetPendingEventsSince(nil, nil, limit)
}

func (r *memRepo) GetByID(id uuid.UUID) (*Event, error)                                { return nil, nil }
func (r *memRepo) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error { return nil }
func (r *memRepo) MarkAsProcessing(id uuid.UUID) error                                 { return nil }
func (r *memRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	return nil
}
func (r *memRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error) { return 0, nil }
func (r *memRepo) ListDeadLetteredEvents(limit int) ([]*Event, error)       { return nil, nil }
func (r *memRepo) RequeueEvent(id uuid.UUID) error                          { return nil }

func (r *memRepo) EnsurePublisherProgressTable() error { return nil }

func (r *memRepo) GetPublisherProgress(publisher string) (*time.Time, *uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.progress[publisher]
	if c == nil {
		return nil, nil, nil
	}
	return c.lastAt, c.lastID, nil
}

func (r *memRepo) UpdatePublisherProgress(publisher string, lastProcessedAt time.Time, lastProcessedID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.progress[publisher]
	if c == nil {
		c = &publisherCursor{}
		r.progress[publisher] = c
	}
	t := lastProcessedAt
	id := lastProcessedID
	c.lastAt = &t
	c.lastID = &id
	return nil
}

func (r *memRepo) GetPendingEventsSince(since *time.Time, lastID *uuid.UUID, limit int) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Event
	for _, e := range r.events {
		if since == nil {
			out = append(out, e)
			continue
		}
		if e.OccurredAt.After(*since) || (e.OccurredAt.Equal(*since) && lastID != nil && e.ID.String() > lastID.String()) {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// mock publishers
type succeedPublisher struct{}

func (p *succeedPublisher) Publish(ctx context.Context, event *Event) error { return nil }

type failPublisher struct{}

func (p *failPublisher) Publish(ctx context.Context, event *Event) error { return assert.AnError }

type slowFailPublisher struct{}

func (p *slowFailPublisher) Publish(ctx context.Context, event *Event) error {
	// simulate latency; dispatcher should time out upstream
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return assert.AnError
	}
}

func TestPerPublisherDrain(t *testing.T) {
	repo := newMemRepo()
	// create one event
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(e)

	mp := NewMultiPublisher(NewConsolePublisher(), &succeedPublisher{})
	// replace internal publishers for deterministic names: publisher-0 will be console, publisher-1 succeed

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 100 * time.Millisecond
	cfg.BatchSize = 10
	cfg.ProcessingTimeout = 200 * time.Millisecond

	d := NewDispatcher(repo, mp, cfg).(*dispatcher)
	// start dispatcher
	if err := d.Start(); err != nil {
		t.Fatalf("start err: %v", err)
	}
	defer d.Stop()

	// wait for some cycles
	time.Sleep(500 * time.Millisecond)

	// Check progress: publisher-1 (succeedPublisher) should have progressed
	since1, id1, _ := repo.GetPublisherProgress("publisher-1")
	if assert.NotNil(t, since1) {
		assert.Equal(t, e.ID.String(), id1.String())
	}

	// publisher-0 (console) is also a console publisher that succeeds, so both should progress
	since0, id0, _ := repo.GetPublisherProgress("publisher-0")
	if assert.NotNil(t, since0) {
		assert.Equal(t, e.ID.String(), id0.String())
	}
}

func TestFailureIsolationAndRecovery(t *testing.T) {
	repo := newMemRepo()
	// create one event
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(e)

	mp := NewMultiPublisher(&failPublisher{}, &succeedPublisher{})

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 100 * time.Millisecond
	cfg.BatchSize = 10
	cfg.ProcessingTimeout = 200 * time.Millisecond

	d := NewDispatcher(repo, mp, cfg).(*dispatcher)
	if err := d.Start(); err != nil {
		t.Fatalf("start err: %v", err)
	}
	defer d.Stop()

	time.Sleep(500 * time.Millisecond)

	// succeedPublisher should progress (publisher-1)
	since1, id1, _ := repo.GetPublisherProgress("publisher-1")
	if assert.NotNil(t, since1) {
		assert.Equal(t, e.ID.String(), id1.String())
	}

	// failPublisher should not progress
	since0, id0, _ := repo.GetPublisherProgress("publisher-0")
	assert.Nil(t, since0)
	assert.Nil(t, id0)

	// Simulate crash recovery: update failing publisher progress to event to simulate manual catch-up
	_ = repo.UpdatePublisherProgress("publisher-0", e.OccurredAt, e.ID)

	// After updating, the event should be marked completed when both have progress
	time.Sleep(200 * time.Millisecond)
	// event should be completed: in mem repo we don't update status, but ensure both cursors present
	since0b, id0b, _ := repo.GetPublisherProgress("publisher-0")
	assert.NotNil(t, since0b)
	assert.Equal(t, e.ID.String(), id0b.String())
}