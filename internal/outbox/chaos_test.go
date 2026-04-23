package outbox

import (
	"database/sql"
	"errors"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// FaultSpec and FaultType
// ---------------------------------------------------------------------------

// FaultType describes the kind of fault to inject.
type FaultType string

const (
	FaultError FaultType = "error"
	FaultDelay FaultType = "delay"
	FaultPanic FaultType = "panic"
)

// FaultSpec describes when and how to inject a fault.
type FaultSpec struct {
	Type        FaultType
	Probability float64       // 0.0–1.0; 1.0 = always fault
	MaxCount    int           // 0 = unlimited; stops faulting after MaxCount faults
	Delay       time.Duration // used when Type == FaultDelay
	Err         error         // used when Type == FaultError
}

// ---------------------------------------------------------------------------
// ChaosPublisher
// ---------------------------------------------------------------------------

// ChaosPublisher wraps a Publisher and injects faults per FaultSpec.
type ChaosPublisher struct {
	inner      Publisher
	spec       FaultSpec
	mu         sync.Mutex
	faultCount int
}

// NewChaosPublisher creates a ChaosPublisher. inner may be nil (no-op on success).
func NewChaosPublisher(inner Publisher, spec FaultSpec) *ChaosPublisher {
	return &ChaosPublisher{inner: inner, spec: spec}
}

// Publish injects a fault according to the FaultSpec, or delegates to inner.
func (c *ChaosPublisher) Publish(event *Event) error {
	c.mu.Lock()
	shouldFault := rand.Float64() < c.spec.Probability
	if shouldFault && c.spec.MaxCount > 0 && c.faultCount >= c.spec.MaxCount {
		shouldFault = false
	}
	if shouldFault {
		c.faultCount++
	}
	c.mu.Unlock()

	if !shouldFault {
		if c.inner != nil {
			return c.inner.Publish(event)
		}
		return nil
	}

	switch c.spec.Type {
	case FaultError:
		if c.spec.Err != nil {
			return c.spec.Err
		}
		return errors.New("chaos: injected error")
	case FaultDelay:
		time.Sleep(c.spec.Delay)
		if c.inner != nil {
			return c.inner.Publish(event)
		}
		return nil
	case FaultPanic:
		panic("chaos: injected panic")
	}
	return nil
}

// FaultCount returns the number of faults injected so far.
func (c *ChaosPublisher) FaultCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.faultCount
}

// ---------------------------------------------------------------------------
// MockRepository — thread-safe in-memory Repository implementation
// ---------------------------------------------------------------------------

// MockRepository is a thread-safe in-memory Repository for testing.
type MockRepository struct {
	mu     sync.Mutex
	events map[uuid.UUID]*Event
	// dedupeKeys tracks dedupe_key → event ID for StoreIfNotExists
	dedupeKeys map[string]uuid.UUID
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		events:     make(map[uuid.UUID]*Event),
		dedupeKeys: make(map[string]uuid.UUID),
	}
}

func (r *MockRepository) clone(e *Event) *Event {
	cp := *e
	return &cp
}

func (r *MockRepository) Store(event *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := r.clone(event)
	r.events[event.ID] = cp
	if event.DedupeKey != "" {
		r.dedupeKeys[event.DedupeKey] = event.ID
	}
	return nil
}

func (r *MockRepository) StoreWithTx(_ *sql.Tx, event *Event) error {
	return r.Store(event)
}

func (r *MockRepository) StoreIfNotExists(event *Event) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.DedupeKey != "" {
		if _, exists := r.dedupeKeys[event.DedupeKey]; exists {
			return false, nil
		}
	}
	cp := r.clone(event)
	r.events[event.ID] = cp
	if event.DedupeKey != "" {
		r.dedupeKeys[event.DedupeKey] = event.ID
	}
	return true, nil
}

func (r *MockRepository) GetPendingEvents(limit int) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var result []*Event
	for _, e := range r.events {
		if e.Status == StatusPending || (e.Status == StatusFailed && e.NextRetryAt != nil && !e.NextRetryAt.After(now)) {
			result = append(result, r.clone(e))
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *MockRepository) GetByID(id uuid.UUID) (*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.events[id]
	if !ok {
		return nil, errors.New("event not found")
	}
	return r.clone(e), nil
}

func (r *MockRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.events[id]
	if !ok {
		return errors.New("event not found")
	}
	e.Status = status
	e.ErrorMessage = errorMessage
	e.UpdatedAt = time.Now()
	return nil
}

func (r *MockRepository) MarkAsProcessing(id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.events[id]
	if !ok {
		return errors.New("event not found")
	}
	if e.Status != StatusPending {
		return errors.New("event not in pending status")
	}
	e.Status = StatusProcessing
	e.UpdatedAt = time.Now()
	return nil
}

func (r *MockRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.events[id]
	if !ok {
		return errors.New("event not found")
	}
	e.RetryCount++
	e.NextRetryAt = &nextRetryAt
	e.Status = StatusFailed
	e.ErrorMessage = errorMessage
	e.UpdatedAt = time.Now()
	return nil
}

func (r *MockRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, e := range r.events {
		if e.Status == StatusCompleted && e.UpdatedAt.Before(olderThan) {
			delete(r.events, id)
			count++
		}
	}
	return count, nil
}

func (r *MockRepository) RecoverStuckEvents(olderThan time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, e := range r.events {
		if e.Status == StatusProcessing && e.UpdatedAt.Before(olderThan) {
			e.Status = StatusPending
			e.RetryCount++
			e.UpdatedAt = time.Now()
			count++
		}
	}
	return count, nil
}

func (r *MockRepository) ListDeadLetterEvents(limit int) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*Event
	for _, e := range r.events {
		if e.Status == StatusFailed && e.RetryCount >= e.MaxRetries {
			result = append(result, r.clone(e))
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (r *MockRepository) RequeueEvent(id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.events[id]
	if !ok {
		return errors.New("event not found")
	}
	if e.Status != StatusFailed {
		return ErrEventNotDeadLettered
	}
	e.Status = StatusPending
	e.RetryCount = 0
	e.NextRetryAt = nil
	e.UpdatedAt = time.Now()
	return nil
}

// ---------------------------------------------------------------------------
// ChaosRepository
// ---------------------------------------------------------------------------

// ChaosRepository wraps a Repository and injects faults on targeted methods.
type ChaosRepository struct {
	inner               Repository
	mu                  sync.Mutex
	storeFault          *FaultSpec
	markProcessingFault *FaultSpec
	updateStatusFault   *FaultSpec
	recoverStuckFault   *FaultSpec
	disabled            atomic.Bool
}

func NewChaosRepository(inner Repository) *ChaosRepository {
	return &ChaosRepository{inner: inner}
}

func (c *ChaosRepository) SetStoreFault(spec *FaultSpec)          { c.mu.Lock(); c.storeFault = spec; c.mu.Unlock() }
func (c *ChaosRepository) SetMarkProcessingFault(spec *FaultSpec) { c.mu.Lock(); c.markProcessingFault = spec; c.mu.Unlock() }
func (c *ChaosRepository) SetUpdateStatusFault(spec *FaultSpec)   { c.mu.Lock(); c.updateStatusFault = spec; c.mu.Unlock() }
func (c *ChaosRepository) SetRecoverStuckFault(spec *FaultSpec)   { c.mu.Lock(); c.recoverStuckFault = spec; c.mu.Unlock() }
func (c *ChaosRepository) SetDisabled(v bool)                     { c.disabled.Store(v) }

func (c *ChaosRepository) maybeFault(spec *FaultSpec) error {
	if spec == nil {
		return nil
	}
	if rand.Float64() < spec.Probability {
		if spec.Err != nil {
			return spec.Err
		}
		return errors.New("chaos: repository fault")
	}
	return nil
}

func (c *ChaosRepository) Store(event *Event) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	c.mu.Lock()
	spec := c.storeFault
	c.mu.Unlock()
	if err := c.maybeFault(spec); err != nil {
		return err
	}
	return c.inner.Store(event)
}

func (c *ChaosRepository) StoreWithTx(tx *sql.Tx, event *Event) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	return c.inner.StoreWithTx(tx, event)
}

func (c *ChaosRepository) StoreIfNotExists(event *Event) (bool, error) {
	if c.disabled.Load() {
		return false, errors.New("chaos: repository disabled")
	}
	return c.inner.StoreIfNotExists(event)
}

func (c *ChaosRepository) GetPendingEvents(limit int) ([]*Event, error) {
	if c.disabled.Load() {
		return nil, errors.New("chaos: repository disabled")
	}
	return c.inner.GetPendingEvents(limit)
}

func (c *ChaosRepository) GetByID(id uuid.UUID) (*Event, error) {
	if c.disabled.Load() {
		return nil, errors.New("chaos: repository disabled")
	}
	return c.inner.GetByID(id)
}

func (c *ChaosRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	c.mu.Lock()
	spec := c.updateStatusFault
	c.mu.Unlock()
	if err := c.maybeFault(spec); err != nil {
		return err
	}
	return c.inner.UpdateStatus(id, status, errorMessage)
}

func (c *ChaosRepository) MarkAsProcessing(id uuid.UUID) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	c.mu.Lock()
	spec := c.markProcessingFault
	c.mu.Unlock()
	if err := c.maybeFault(spec); err != nil {
		return err
	}
	return c.inner.MarkAsProcessing(id)
}

func (c *ChaosRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	return c.inner.IncrementRetryCount(id, nextRetryAt, errorMessage)
}

func (c *ChaosRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	if c.disabled.Load() {
		return 0, errors.New("chaos: repository disabled")
	}
	return c.inner.DeleteCompletedEvents(olderThan)
}

func (c *ChaosRepository) RecoverStuckEvents(olderThan time.Time) (int64, error) {
	if c.disabled.Load() {
		return 0, errors.New("chaos: repository disabled")
	}
	c.mu.Lock()
	spec := c.recoverStuckFault
	c.mu.Unlock()
	if err := c.maybeFault(spec); err != nil {
		return 0, err
	}
	return c.inner.RecoverStuckEvents(olderThan)
}

func (c *ChaosRepository) ListDeadLetterEvents(limit int) ([]*Event, error) {
	if c.disabled.Load() {
		return nil, errors.New("chaos: repository disabled")
	}
	return c.inner.ListDeadLetterEvents(limit)
}

func (c *ChaosRepository) RequeueEvent(id uuid.UUID) error {
	if c.disabled.Load() {
		return errors.New("chaos: repository disabled")
	}
	return c.inner.RequeueEvent(id)
}

// ---------------------------------------------------------------------------
// Helper: wait for condition with timeout
// ---------------------------------------------------------------------------

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// Task 7.3: TestProcessCrashMidFlight
// Feature: outbox-hardening, Requirement 6.4
// ---------------------------------------------------------------------------

// TestProcessCrashMidFlight verifies that an event stuck in processing status
// (simulating a process crash mid-flight) is recovered and eventually completed.
func TestProcessCrashMidFlight(t *testing.T) {
	// Feature: outbox-hardening, Requirement 6.4: process crash mid-flight recovery
	repo := NewMockRepository()
	publisher := NewMockPublisher()

	// Store an event and immediately mark it as processing (simulating crash).
	event, err := NewEvent("crash.test", map[string]string{"k": "v"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event))
	require.NoError(t, repo.MarkAsProcessing(event.ID))

	// Use a very short ProcessingTimeout so RecoverStuckEvents picks it up quickly.
	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 30 * time.Millisecond
	cfg.ProcessingTimeout = 20 * time.Millisecond // shorter than poll interval
	cfg.MaxRetries = 3

	d := NewDispatcher(repo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	// Wait for the event to be recovered and completed.
	ok := waitFor(t, 2*time.Second, func() bool {
		e, err := repo.GetByID(event.ID)
		return err == nil && e.Status == StatusCompleted
	})
	assert.True(t, ok, "event should reach completed status after crash recovery")
}

// ---------------------------------------------------------------------------
// Task 7.4: TestDBFailoverRecovery
// Feature: outbox-hardening, Requirement 6.5
// ---------------------------------------------------------------------------

// TestDBFailoverRecovery verifies that pending events are eventually published
// after a simulated DB failover window.
func TestDBFailoverRecovery(t *testing.T) {
	// Feature: outbox-hardening, Requirement 6.5: DB failover recovery
	inner := NewMockRepository()
	chaosRepo := NewChaosRepository(inner)
	publisher := NewMockPublisher()

	// Store 3 pending events in the inner (real) repository before disabling.
	var eventIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		event, err := NewEvent("failover.test", map[string]int{"i": i}, nil, nil)
		require.NoError(t, err)
		require.NoError(t, inner.Store(event))
		eventIDs = append(eventIDs, event.ID)
	}

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 30 * time.Millisecond
	cfg.ProcessingTimeout = 500 * time.Millisecond
	cfg.MaxRetries = 10 // high so retries don't exhaust during failover

	d := NewDispatcher(chaosRepo, publisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	// Disable the repository to simulate DB failover for ~3 poll cycles.
	chaosRepo.SetDisabled(true)
	time.Sleep(100 * time.Millisecond)

	// Re-enable the repository.
	chaosRepo.SetDisabled(false)

	// All events should eventually reach completed status.
	ok := waitFor(t, 3*time.Second, func() bool {
		for _, id := range eventIDs {
			e, err := inner.GetByID(id)
			if err != nil || e.Status != StatusCompleted {
				return false
			}
		}
		return true
	})
	assert.True(t, ok, "all events should reach completed status after DB failover recovery")
}

// ---------------------------------------------------------------------------
// Task 7.5: TestRetryStormPrevention
// Feature: outbox-hardening, Property 14
// ---------------------------------------------------------------------------

// TestRetryStormPrevention verifies that jitter in CalculateNextRetry prevents
// synchronized retry storms: the standard deviation of next_retry_at values
// across 50 simultaneously-failing events must be >= RetryBaseDelay/2.
func TestRetryStormPrevention(t *testing.T) {
	// Feature: outbox-hardening, Property 14: Retry storm prevention via jitter spread
	// Validates: Requirements 6.6
	repo := NewMockRepository()

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 10 * time.Millisecond
	cfg.RetryBaseDelay = 100 * time.Millisecond
	cfg.RetryBackoffFactor = 2.0
	cfg.MaxRetries = 10
	cfg.BatchSize = 60

	// Create 50 events.
	var eventIDs []uuid.UUID
	for i := 0; i < 50; i++ {
		event, err := NewEvent("storm.test", map[string]int{"i": i}, nil, nil)
		require.NoError(t, err)
		require.NoError(t, repo.Store(event))
		eventIDs = append(eventIDs, event.ID)
	}

	// ChaosPublisher always returns an error (simulates all events failing).
	chaosPublisher := NewChaosPublisher(nil, FaultSpec{
		Type:        FaultError,
		Probability: 1.0,
		Err:         errors.New("simulated failure"),
	})

	d := NewDispatcher(repo, chaosPublisher, cfg)
	require.NoError(t, d.Start())

	// Wait for all events to have been attempted at least once (retry_count > 0).
	ok := waitFor(t, 3*time.Second, func() bool {
		for _, id := range eventIDs {
			e, err := repo.GetByID(id)
			if err != nil || e.RetryCount == 0 {
				return false
			}
		}
		return true
	})
	d.Stop()
	require.True(t, ok, "all events should have been attempted at least once")

	// Collect next_retry_at values.
	var retryTimes []float64
	for _, id := range eventIDs {
		e, err := repo.GetByID(id)
		require.NoError(t, err)
		if e.NextRetryAt != nil {
			retryTimes = append(retryTimes, float64(e.NextRetryAt.UnixNano()))
		}
	}
	require.NotEmpty(t, retryTimes, "events should have next_retry_at set")

	// Compute standard deviation.
	stddev := stdDev(retryTimes)
	minStddev := float64(cfg.RetryBaseDelay) / 2.0

	assert.GreaterOrEqual(t, stddev, minStddev,
		"stddev of next_retry_at (%.0f ns) should be >= RetryBaseDelay/2 (%.0f ns) to prevent retry storms",
		stddev, minStddev)
}

// stdDev computes the population standard deviation of a float64 slice.
func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var variance float64
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

// ---------------------------------------------------------------------------
// Task 7.6: TestPanicRecovery
// Feature: outbox-hardening, Requirement 6.7
// ---------------------------------------------------------------------------

// TestPanicRecovery verifies that the Dispatcher recovers from a publisher panic
// and continues processing subsequent events.
func TestPanicRecovery(t *testing.T) {
	// Feature: outbox-hardening, Requirement 6.7: panic recovery in dispatcher
	repo := NewMockRepository()

	// Create 2 events.
	event1, err := NewEvent("panic.test", map[string]string{"n": "1"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event1))

	event2, err := NewEvent("panic.test", map[string]string{"n": "2"}, nil, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Store(event2))

	// ChaosPublisher panics on the first call only, then succeeds.
	chaosPublisher := NewChaosPublisher(nil, FaultSpec{
		Type:        FaultPanic,
		Probability: 1.0,
		MaxCount:    1, // panic only once
	})

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 30 * time.Millisecond
	cfg.ProcessingTimeout = 500 * time.Millisecond
	cfg.MaxRetries = 5

	d := NewDispatcher(repo, chaosPublisher, cfg)
	require.NoError(t, d.Start())
	defer d.Stop()

	// Wait for event2 to reach completed status (dispatcher survived the panic).
	ok := waitFor(t, 3*time.Second, func() bool {
		e, err := repo.GetByID(event2.ID)
		return err == nil && e.Status == StatusCompleted
	})
	assert.True(t, ok, "event2 should reach completed status after dispatcher recovers from panic")

	// Dispatcher should still be running.
	assert.True(t, d.IsRunning(), "dispatcher should still be running after panic recovery")
}
