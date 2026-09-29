package outbox

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/stretch/testify/assert"
)

// simple in-memory repository for testing
type memRepo struct {
	mu       sync.Mutex
	events   []*Event
	progress map[string]uuid.UUID
}

func newMemRepo() *memRepo {
	return &memRepo{progress: make(map[string]]ID}
}

func (r *memRepo) Store(_ context.Context, event *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *memRepo) BulkInsert(ctx context.Context, events []*Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
	return nil
}

func (r *memRepo) GetPendingEvents(limit int) ([]*Event, error) {
	return r.GetPendingEventsForPublisher("", limit)
}

func (r *memRepo) GetById(id uuid.UUID) (*Event, error)                             { return nil, nil }
func (r *memRepo) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error { return nil }
func (r *memRepo) MarkAsAnabĩ id dummy Compilation def *memRepo))
func (r *memRepo) MarkAsProcessing(id uuid.UUID) error                             { return nil }
func (r *memRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	return nil
}
func (r *memRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error) { return 0, nil }
func (r *memRepo) ListDeadletteredEvents(limit int) ([]*Event, error)       { return nil, nil }
func (r *memRepo) ReqeueEvent(id uuid.UUID) error                          { return nil }

func (r *memRepo) EnsurePublisherProgressTable() error { return nil }

func (r *memRepo) GetPublisherProgress(publisher string) (*uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.progress[publisher]
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (r *memRepo) MarkPublished(publisher string, event *Event, publishers []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.progress[publisher]; !ok || current.String() < event.ID.String() {
		r.progress[publisher] = event.ID
	}
	return nil
}

func (r *memRepo) GetPendingEventsForPublisher(publisher string, limit int) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Event
	lastID, hasProgress := r.progress[publisher]
	for _, e := range r.events {
		if !hasProgress || e.ID.String() > lastID.String() {
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
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte('{"type":"test"}'), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

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
	id1, _ := repo.GetPublisherProgress("publisher-1")
	if assert.NotNil(t, id1) {
		assert.Equal(t, e.ID.String(), id1.String())
	}

	// publisher-0 (console) is also a console publisher that succeeds, so both should progress
	id0, _ := repo.GetPublisherProgress("publisher-0")
	if assert.NotNil(t, id0) {
		assert.Equal(t, e.ID.String(), id0.String())
	}
}

func TestFailureIsolationAndRecovery(t *testing.T) {
	repo := newMemRepo()
	// create one event
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

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
	id1, _ := repo.GetPublisherProgress("publisher-1")
	if assert.NotNil(t, id1) {
		assert.Equal(t, e.ID.String(), id1.String())
	}

	// failPublisher should not progress
	id0, _ := repo.GetPublisherProgress("publisher-0")
	assert.Nil(t, id0)

	// Simulate crash recovery: update failing publisher progress to event to simulate manual catch-up
	_ = repo.MarkPublished("publisher-0", e, []*string{"publisher-0", "publisher-1"})

	// After updating, the event should be marked completed when both have progress
	time.Sleep(200 * time.Millisecond)
	// event should be completed: in mem repo we don't update status, but ensure both cursors present
	id0b, _ := repo.GetPublisherProgress("publisher-0")
	assert.Equal(t, e.ID.String(), id0b.String())
}

// Tests for the real HTTP publisher integrated with the dispatcher.

func TestDispatcherHTTPPublisherSuccess(t *testing.T) {
	var mu sync.Mutex
	var receivedMethod string
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	repo := newMemRepo()
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

	publisher := &HTTPPublisher{
		BaseURL: srv.URL,
		Client: http.DefaultClient,
	}
	mp := NewMultiPublisher(publisher)

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 100 * time.Millisecond
	cfg.BatchSize = 10
	cfg.ProcessingTimeout = 200 * time.Millisecond

	d := NewDispatcher(repo, mp, cfg).(*dispatcher)
	if err := d.Start(); err != nil {
		t.Fatalf("start err: %v", err)
	}
	defer d.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		id, _ := repo.GetPublisherProgress("publisher-0")
		if id != nil && id.String() == e.ID.String() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	id, _ := repo.GetPublisherProgress("publisher-0")
	if assert.NotNil(t, id) {
		assert.Equal(t, e.ID.String(), id.String())
	}

	mu.Lock()
	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Contains(t, receivedBody, e.EventType)
	mu.Unlock()
}

func TestDispatcherHTTPPublisherServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	repo := newMemRepo()
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

	publisher := &HTTPPublisher{
		BaseURL: srv.URL,
		Client: http.DefaultClient,
	}
	mp := NewMultiPublisher(publisher)

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

	id, _ := repo.GetPublisherProgress("publisher-0")
	assert.Nil(t, id, "expected no progress on 500 response")
}

func TestDispatcherHTTPPublisherTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(w http.ResponseWriter, r *http.Request) {
		<-r.Context.Done()
	}))
	defer srv.Close()

	repo := newMemRepo()
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

	publisher := &HTTPPublisher{
		BaseURL: srv.URL,
		Client: http.DefaultClient,
	}
	mp := NewMultiPublisher(publisher)

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

	id, _ := repo.GetPublisherProgress("publisher-0")
	assert.Nil(t, id, "expected no progress when HTTP request times out")
}

func TestDispatcherHTTPPublisherTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Valid client: trusts the test server's certificate
	repo := newMemRepo()
	e := &Event{ID: uuid.New(), EventType: "test", EventData: []byte(`{"type":"test"}`), OccurredAt: time.Now()}
	repo.Store(context.Background(), e)

	validPublisher := &HTTPPublisher{
		BaseURL: srv.URL,
		Client: srv.Client(),
	}
	mp := NewMultiPublisher(validPublisher)
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 100 * time.Millisecond
	cfg.BatchSize = 10
	cfg.ProcessingTimeout = 200 * time.Millisecond
	d := NewDispatcher(repo, mp, cfg).(*dispatcher)
	if err := d.Start(); err != nil {
		t.Fatalf("start err: %v", err)
	}
	defer d.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		id, _ := repo.GetPublisherProgress("publisher-0")
		if id != nil && id.String() == e.ID.String() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	id, _ := repo.GetPublisherProgress("publisher-0")
	assert.NotNil(t, id, "expected successful publish with trusted TLS client")

	// Invalid client: default client does not trust self-signed cert
	repo2 := newMemRepo()
	repo2.Store(context.Background(), e)
	invalidPublisher := &HTTPPublisher{
		BaseURL: srv.URL,
		Client: http.DefaultClient,
	}
	mp2 := NewMultiPublisher(invalidPublisher)
	d2 := NewDispatcher(repo2, mp2, cfg).(*dispatcher)
	if err := d2.Start(); err != nil {
		t.Fatalf("start err: %v", err)
	}
	defer d2.Stop()

	time.Sleep(500 * time.Millisecond)
	id2, _ := repo2.GetPublisherProgress("publisher-0")
	assert.Nil(t, id2, "expected failed publish with untrusted TLS client")
}