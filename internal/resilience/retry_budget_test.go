package resilience

import (
	"sync"
	"testing"
	"time"
)

func TestDefaultRetryBudget(t *testing.T) {
	b := DefaultRetryBudget()
	if b.Ratio != 0.10 {
		t.Errorf("expected ratio 0.10, got %f", b.Ratio)
	}
	if b.WindowSize != 30*time.Second {
		t.Errorf("expected window 30s, got %v", b.WindowSize)
	}
	if b.Buckets != 10 {
		t.Errorf("expected 10 buckets, got %d", b.Buckets)
	}
}

func TestRetryBudgetAllowRetryOnEmptyBudget(t *testing.T) {
	b := DefaultRetryBudget()
	// No successes recorded yet → AllowRetry should return false
	if b.AllowRetry() {
		t.Error("expected AllowRetry to return false with no successes")
	}
}

func TestRetryBudgetAllowsRetryUnderThreshold(t *testing.T) {
	b := DefaultRetryBudget()

	// Record 100 successes
	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}

	// 8 retries = 8% which is under the 10% threshold
	for i := 0; i < 8; i++ {
		if !b.AllowRetry() {
			t.Fatalf("expected AllowRetry to be true at retry %d (8%%)", i+1)
		}
		b.RecordRetry()
	}
}

func TestRetryBudgetDeniesRetryOverThreshold(t *testing.T) {
	b := DefaultRetryBudget()

	// Record 100 successes
	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}

	// Record 10 retries = 10% which is exactly the threshold
	for i := 0; i < 10; i++ {
		if !b.AllowRetry() {
			t.Fatalf("expected AllowRetry to be true at retry %d (10 percent)", i+1)
		}
		b.RecordRetry()
	}

	// At exactly threshold, AllowRetry still returns true
	if !b.AllowRetry() {
		t.Error("expected AllowRetry true at exactly the threshold")
	}
	b.RecordRetry() // This is retry #11

	// 12th retry should be denied since 11/100 = 11% > 10%
	if b.AllowRetry() {
		t.Error("expected AllowRetry to be false at 12 retries (11 percent)")
	}
}

func TestRetryBudgetAvailableRatio(t *testing.T) {
	b := DefaultRetryBudget()

	// No activity: available should be 0 (no successes)
	if avail := b.Available(); avail != 0 {
		t.Errorf("expected Available 0 with no activity, got %f", avail)
	}

	// Record 100 successes
	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}

	// Available should be 1.0 (no retries)
	if avail := b.Available(); avail != 1.0 {
		t.Errorf("expected Available 1.0 with no retries, got %f", avail)
	}

	// Record 5 retries = 5% of 100 successes → 50% budget remaining
	for i := 0; i < 5; i++ {
		b.RecordRetry()
	}
	avail := b.Available()
	if avail < 0.49 || avail > 0.51 {
		t.Errorf("expected Available ~0.5 with 5 percent retries, got %f", avail)
	}
}

func TestRetryBudgetWindowSlides(t *testing.T) {
	b := DefaultRetryBudget()

	// Record 100 successes and 15 retries (15% > 10%) → budget exhausted
	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}
	for i := 0; i < 15; i++ {
		b.RecordRetry()
	}

	if b.AllowRetry() {
		t.Error("expected AllowRetry to be false with 15% retries")
	}
}

func TestRetryBudgetSnapshot(t *testing.T) {
	b := DefaultRetryBudget()

	b.RecordSuccess()
	b.RecordSuccess()
	b.RecordRetry()

	snap := b.Snapshot()
	if snap.SuccessTotal != 2 {
		t.Errorf("expected 2 successes, got %d", snap.SuccessTotal)
	}
	if snap.RetriesTotal != 1 {
		t.Errorf("expected 1 retry, got %d", snap.RetriesTotal)
	}
	if snap.Ratio != 0.10 {
		t.Errorf("expected ratio 0.10, got %f", snap.Ratio)
	}
	if snap.String() == "" {
		t.Error("expected non-empty String()")
	}
}

func TestRetryBudgetConcurrentAccess(t *testing.T) {
	b := DefaultRetryBudget()

	var wg sync.WaitGroup
	n := 50

	// Concurrent successes
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.RecordSuccess()
		}()
	}

	// Concurrent retries
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.RecordRetry()
		}()
	}

	// Concurrent AllowRetry
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.AllowRetry()
		}()
	}

	wg.Wait()

	snap := b.Snapshot()
	t.Logf("Snapshot after concurrency: %s", snap)
}

func TestRetryBudgetWindowSlidesOverTime(t *testing.T) {
	b := DefaultRetryBudget()
	b.WindowSize = 100 * time.Millisecond
	b.Buckets = 4

	// Record 100 successes and 15 retries in the first window
	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}
	for i := 0; i < 15; i++ {
		b.RecordRetry()
	}

	if b.AllowRetry() {
		t.Error("expected AllowRetry false when budget exhausted")
	}

	// Wait for the window to slide past all data
	time.Sleep(150 * time.Millisecond)

	// Record more successes, retries should now be allowed
	b.RecordSuccess()

	if !b.AllowRetry() {
		t.Error("expected AllowRetry true after window slid")
	}
}

func TestRetryBudgetWithRatioOne(t *testing.T) {
	b := DefaultRetryBudget()
	b.Ratio = 1.0 // Retries allowed up to success count

	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}

	// With ratio=1.0, 100 successes allows 101 retries (100 + 1 for exact match)
	for i := 0; i < 101; i++ {
		if !b.AllowRetry() {
			t.Fatalf("expected AllowRetry true with ratio=1.0 at retry %d", i+1)
		}
		b.RecordRetry()
	}

	// 102nd should be denied (101/100 = 1.01 > 1.0)
	if b.AllowRetry() {
		t.Error("expected AllowRetry false when retries exceed successes")
	}
}

func TestRetryBudgetAllowsExactlyAtThreshold(t *testing.T) {
	b := DefaultRetryBudget()
	b.Ratio = 0.5 // 50% threshold

	for i := 0; i < 100; i++ {
		b.RecordSuccess()
	}
	// 50 retries = exactly 50% → should be allowed
	for i := 0; i < 50; i++ {
		if !b.AllowRetry() {
			t.Fatalf("expected AllowRetry true at threshold at retry %d", i+1)
		}
		b.RecordRetry()
	}

	// At exactly threshold, AllowRetry should still return true (<=)
	if !b.AllowRetry() {
		t.Error("expected AllowRetry true when at exactly the threshold")
	}
	b.RecordRetry() // This is retry #51

	// 52nd should be denied (51/100 = 51% > 50%)
	if b.AllowRetry() {
		t.Error("expected AllowRetry false when exceeding threshold")
	}
}

func TestRetryBudgetLazyInit(t *testing.T) {
	// Create a zero-value RetryBudget and ensure it works
	var b RetryBudget

	b.RecordSuccess()
	b.RecordSuccess()

	if !b.AllowRetry() {
		t.Error("expected AllowRetry true after 2 successes with default budget")
	}
}
