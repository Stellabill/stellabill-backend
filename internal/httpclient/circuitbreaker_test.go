package httpclient

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	if cb.State() != StateClosed {
		t.Fatalf("expected closed, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatalf("expected allow")
	}

	// Fail 3 times
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}
	if cb.Allow() {
		t.Fatalf("expected not allow")
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected half open, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatalf("expected allow for probe request")
	}
	// Second concurrent request should fail fast while probe is running
	if cb.Allow() {
		t.Fatalf("expected not allow for subsequent requests in half-open state")
	}

	// Record success should close it
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Fatalf("expected closed after success, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 50*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected half open")
	}
	if !cb.Allow() {
		t.Fatalf("expected allow for probe request")
	}

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatalf("expected open after probe failure")
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	cb := NewCircuitBreaker(10, 100*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.Allow()
			cb.RecordSuccess()
			cb.RecordFailure()
			cb.State()
		}()
	}
	wg.Wait()
}

