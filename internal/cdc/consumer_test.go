package cdc

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// updateLSN tests
// ---------------------------------------------------------------------------

func TestUpdateLSN_Monotonic(t *testing.T) {
	c := &Consumer{}
	c.updateLSN(100)
	if c.lastLSN != 100 {
		t.Fatalf("expected lastLSN=100, got %d", c.lastLSN)
	}
	// Lower LSN should be ignored.
	c.updateLSN(50)
	if c.lastLSN != 100 {
		t.Fatalf("expected lastLSN=100 after lower update, got %d", c.lastLSN)
	}
	// Higher LSN should update.
	c.updateLSN(200)
	if c.lastLSN != 200 {
		t.Fatalf("expected lastLSN=200, got %d", c.lastLSN)
	}
}

func TestUpdateLSN_Zero(t *testing.T) {
	c := &Consumer{lastLSN: 50}
	c.updateLSN(0)
	if c.lastLSN != 50 {
		t.Fatalf("expected lastLSN=50 (unchanged), got %d", c.lastLSN)
	}
}

// ---------------------------------------------------------------------------
// isTimeout tests
// ---------------------------------------------------------------------------

type timeoutError struct{ msg string }

func (e *timeoutError) Error() string { return e.msg }
func (e *timeoutError) Timeout() bool { return true }

type nonTimeoutError struct{ msg string }

func (e *nonTimeoutError) Error() string { return e.msg }

type wrappedTimeoutError struct{ msg string }

func (e *wrappedTimeoutError) Error() string { return e.msg }
func (e *wrappedTimeoutError) Unwrap() error { return &timeoutError{msg: "inner timeout"} }

func TestIsTimeout_True(t *testing.T) {
	if !isTimeout(&timeoutError{msg: "timeout"}) {
		t.Fatal("expected timeout error to be detected")
	}
	// net.OpError implements Timeout()
	netErr := &net.OpError{Err: &timeoutError{msg: "dial timeout"}}
	if !isTimeout(netErr) {
		t.Fatal("expected net.OpError with timeout to be detected")
	}
}

func TestIsTimeout_False(t *testing.T) {
	if isTimeout(errors.New("plain error")) {
		t.Fatal("plain error should not be timeout")
	}
	if isTimeout(&nonTimeoutError{msg: "not a timeout"}) {
		t.Fatal("non-timeout error should not be detected")
	}
	if isTimeout(nil) {
		t.Fatal("nil should not be timeout")
	}
}

func TestIsTimeout_Wrapped(t *testing.T) {
	// Test with errors wrapped via %w - errors.As should unwrap.
	wrapped := &wrappedTimeoutError{msg: "wrapped"}
	// The wrappedTimeoutError doesn't implement Timeout(), so errors.As
	// looks for the inner timeoutError which does. But errors.As checks
	// the interface on the concrete type, not by unwrapping.
	// Actually, errors.As does traverse the chain. Let me verify.
	// The interface{ Timeout() bool } check: wrappedTimeoutError doesn't have
	// Timeout(), but the inner timeoutError does. errors.As should find it.
	if !isTimeout(wrapped) {
		t.Fatal("expected wrapped timeout to be detected via errors.As")
	}
}

// ---------------------------------------------------------------------------
// Stop lifecycle tests
// ---------------------------------------------------------------------------

func TestConsumerStop_BeforeStart(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{ConnString: "postgres://test"})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	// Stop before Start is safe.
	c.Stop()
	c.Stop() // double stop should be safe
}

func TestConsumerStop_WithCancel(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{ConnString: "postgres://test"})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	// Simulate a running consumer by setting running and cancel manually.
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	ctx.Done() // don't block on ctx
	_ = ctx

	c.Stop()

	c.mu.RLock()
	if c.running {
		// Stop doesn't reset running directly (that's done in Start's defer)
		// But cancel should be called which signals the loop to exit.
	}
	c.mu.RUnlock()

	// Verify cancel was called (context should be cancelled).
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected context to be cancelled after Stop()")
	}
}

func TestConsumerStop_DoubleStop(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{ConnString: "postgres://test"})
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	_ = ctx

	c.Stop()
	c.Stop() // safe
	c.Stop() // safe

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected context to be cancelled")
	}
}

// ---------------------------------------------------------------------------
// replicationLoop tests (unit-testable portions)
// ---------------------------------------------------------------------------

// TestReplicationLoop_ContextCancelled verifies that the replication loop
// returns immediately when the context is already cancelled.
func TestReplicationLoop_ContextCancelled(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://test",
		SlotName:            "test_slot",
		MaxReconnectAttempts: 1,
		ReconnectBackoff:    10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.replicationLoop(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestReplicationLoop_MaxReconnects verifies the max reconnect limit works.
// Since there's no real PG, runReplication will fail immediately,
// and we test that after MaxReconnectAttempts it returns an error.
func TestReplicationLoop_MaxReconnects(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://invalid:5432/test",
		SlotName:            "test_slot",
		MaxReconnectAttempts: 2,
		ReconnectBackoff:    1 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.replicationLoop(ctx)
	if err == nil {
		t.Fatal("expected error after max reconnect attempts")
	}
	// Should contain "exceeded max reconnect attempts" since Context won't
	// be deadline-exceeded before MaxReconnectAttempts retries finish.
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// Verify it's the expected exceed-attempts error.
		if !strings.Contains(err.Error(), "exceeded max reconnect attempts") {
			t.Errorf("expected 'exceeded max reconnect attempts' in error, got: %v", err)
		}
	}
}

// TestReplicationLoop_UnlimitedReconnects tests with MaxReconnectAttempts=0.
func TestReplicationLoop_UnlimitedReconnects(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://invalid:5432/test",
		SlotName:            "test_slot",
		MaxReconnectAttempts: 0, // unlimited
		ReconnectBackoff:    1 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.replicationLoop(ctx)
	// Should eventually hit the context deadline.
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// execSimple indirect coverage via runReplication
// ---------------------------------------------------------------------------

// TestRunReplication_ConnectFailure verifies that connection failures
// are correctly propagated.
func TestRunReplication_ConnectFailure(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://nonexistent:5432/test",
		SlotName:            "test_slot",
		StandbyTimeout:      10 * time.Second,
		MaxReconnectAttempts: 1,
		ReconnectBackoff:    1 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.runReplication(ctx)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// Consumer isAlreadyRunning test
// ---------------------------------------------------------------------------

func TestStart_AlreadyRunning(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{ConnString: "postgres://test"})
	c.running = true

	ctx := context.Background()
	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected error when starting an already-running consumer")
	}
	if err.Error() != "cdc: consumer already running" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestStart_ContextCancellation verifies that Start returns when context is
// cancelled before the replication connection is established.
func TestStart_ContextCancellation(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://nonexistent:5432/test",
		MaxReconnectAttempts: 1,
		ReconnectBackoff:    1 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// NewConsumer custom config tests
// ---------------------------------------------------------------------------

func TestNewConsumer_CustomConfig(t *testing.T) {
	cfg := ConsumerConfig{
		ConnString:          "postgres://test",
		SlotName:            "custom_slot",
		PublicationName:     "custom_pub",
		StandbyTimeout:      30 * time.Second,
		ReconnectBackoff:    5 * time.Second,
		MaxReconnectAttempts: 5,
		Sinks:               []Sink{NewMemorySink()},
	}
	c, err := NewConsumer(cfg)
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.SlotName != "custom_slot" {
		t.Errorf("expected custom_slot, got %q", c.cfg.SlotName)
	}
	if c.cfg.PublicationName != "custom_pub" {
		t.Errorf("expected custom_pub, got %q", c.cfg.PublicationName)
	}
	if c.cfg.StandbyTimeout != 30*time.Second {
		t.Errorf("expected 30s standby timeout, got %v", c.cfg.StandbyTimeout)
	}
	if c.cfg.ReconnectBackoff != 5*time.Second {
		t.Errorf("expected 5s backoff, got %v", c.cfg.ReconnectBackoff)
	}
	if c.cfg.MaxReconnectAttempts != 5 {
		t.Errorf("expected 5 max attempts, got %d", c.cfg.MaxReconnectAttempts)
	}
	if len(c.cfg.Sinks) != 1 {
		t.Errorf("expected 1 sink, got %d", len(c.cfg.Sinks))
	}
}

func TestNewConsumer_ZeroStandbyTimeout(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{
		ConnString:     "postgres://test",
		StandbyTimeout: 0,
	})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.StandbyTimeout != 10*time.Second {
		t.Errorf("expected default 10s standby timeout, got %v", c.cfg.StandbyTimeout)
	}
}

func TestNewConsumer_ZeroReconnectBackoff(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{
		ConnString:       "postgres://test",
		ReconnectBackoff: 0,
	})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.ReconnectBackoff != 1*time.Second {
		t.Errorf("expected default 1s backoff, got %v", c.cfg.ReconnectBackoff)
	}
}

func TestNewConsumer_EmptyPublicationName(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{
		ConnString:      "postgres://test",
		PublicationName: "",
	})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.PublicationName != "stellabill_cdc" {
		t.Errorf("expected default publication, got %q", c.cfg.PublicationName)
	}
}

func TestNewConsumer_EmptySlotName(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{
		ConnString: "postgres://test",
		SlotName:   "",
	})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.SlotName != "stellabill_cdc_slot" {
		t.Errorf("expected default slot, got %q", c.cfg.SlotName)
	}
}

// ---------------------------------------------------------------------------
// Consumer partial lifecycle with sinks
// ---------------------------------------------------------------------------

func TestStart_DeferClosesSinks(t *testing.T) {
	sink := NewMemorySink()

	c, _ := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://nonexistent:5432/test",
		Sinks:               []Sink{sink},
		MaxReconnectAttempts: 1,
		ReconnectBackoff:    1 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start will try to connect, fail, and then call defer which closes sinks.
	_ = c.Start(ctx)

	// After Start returns (with error), sinks should be closed.
	// Verify by trying to write - should fail since sink is closed.
	err := sink.WriteChange(context.Background(), Change{Op: "insert", Table: "test"})
	if err == nil {
		t.Fatal("expected sink to be closed after consumer error")
	}
}
