package security

import (
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/timeutil"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestLockoutTracker_RecordFailure_StartsLockout(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected account to be locked after first failure")
	}
	if lt.Attempts("10.0.0.1", "admin") != 1 {
		t.Fatalf("expected 1 attempt, got %d", lt.Attempts("10.0.0.1", "admin"))
	}
	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem <= 0 || rem > time.Second {
		t.Fatalf("expected lockout remaining ~1s, got %v", rem)
	}
}

func TestLockoutTracker_ExpiresAfterDuration(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(2 * time.Second)
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected lockout to expire after 1s")
	}
}

func TestLockoutTracker_ExponentialBackoff(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	clock.Advance(2 * time.Second)

	lt.RecordFailure("10.0.0.1", "admin")
	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected locked after 2nd failure")
	}
	clock.Advance(2 * time.Second)
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected lockout to expire after 2s (2nd failure = 2s lockout)")
	}
}

func TestLockoutTracker_CappedAtMaxLockout(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)
	lt := NewLockoutTracker(WithClock(clock), WithMaxLockout(10*time.Second))

	for i := 0; i < 20; i++ {
		lt.RecordFailure("10.0.0.1", "admin")
	}

	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem > 11*time.Second {
		t.Fatalf("expected lockout capped at ~10s, got %v", rem)
	}
	if rem < 9*time.Second {
		t.Fatalf("expected lockout remaining ~10s, got %v", rem)
	}
}

func TestLockoutTracker_Reset(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected locked after failure")
	}

	lt.Reset("10.0.0.1", "admin")
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected unlocked after reset")
	}
	if lt.Attempts("10.0.0.1", "admin") != 0 {
		t.Fatal("expected attempts reset to 0")
	}
	if lt.LockoutRemaining("10.0.0.1", "admin") != 0 {
		t.Fatal("expected lockout remaining 0 after reset")
	}
}

func TestLockoutTracker_ResetRestartsBackoff(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	lt.RecordFailure("10.0.0.1", "admin")
	lt.Reset("10.0.0.1", "admin")

	lt.RecordFailure("10.0.0.1", "admin")
	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem > 2*time.Second {
		t.Fatalf("expected lockout to restart at 1s after reset, got %v", rem)
	}
}

func TestLockoutTracker_IsolatedBySource(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected 10.0.0.1/admin to be locked")
	}
	if lt.IsLocked("10.0.0.2", "admin") {
		t.Fatal("expected 10.0.0.2/admin to not be locked")
	}
	if lt.IsLocked("10.0.0.1", "root") {
		t.Fatal("expected 10.0.0.1/root to not be locked")
	}
}

func TestLockoutTracker_CleanupExpiredEntries(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(2 * time.Second)
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected lockout expired")
	}

	if lt.Attempts("10.0.0.1", "admin") != 0 {
		t.Fatal("expected expired entry to be cleaned up")
	}
}

func TestLockoutTracker_IsLockedUnknown(t *testing.T) {
	lt := NewLockoutTracker()
	if lt.IsLocked("unknown", "nobody") {
		t.Fatal("expected unknown key to not be locked")
	}
}

func TestLockoutTracker_LockoutRemainingUnknown(t *testing.T) {
	lt := NewLockoutTracker()
	if rem := lt.LockoutRemaining("unknown", "nobody"); rem != 0 {
		t.Fatalf("expected 0 for unknown key, got %v", rem)
	}
}

func TestLockoutTracker_AttemptsUnknown(t *testing.T) {
	lt := NewLockoutTracker()
	if n := lt.Attempts("unknown", "nobody"); n != 0 {
		t.Fatalf("expected 0 for unknown key, got %d", n)
	}
}

func TestLockoutTracker_ConcurrentAccess(t *testing.T) {
	lt := NewLockoutTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			src := "10.0.0.1"
			acct := "admin"
			if n%2 == 0 {
				src = "10.0.0.2"
				acct = "root"
			}
			lt.RecordFailure(src, acct)
			lt.IsLocked(src, acct)
			lt.LockoutRemaining(src, acct)
			lt.Attempts(src, acct)
		}(i)
	}
	wg.Wait()
}

func TestLockoutTracker_ConcurrentReset(t *testing.T) {
	lt := NewLockoutTracker()
	lt.RecordFailure("10.0.0.1", "admin")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lt.Reset("10.0.0.1", "admin")
			lt.IsLocked("10.0.0.1", "admin")
		}()
	}
	wg.Wait()
}

func TestLockoutTracker_WithBaseLockout(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock), WithBaseLockout(5*time.Second))

	lt.RecordFailure("10.0.0.1", "admin")
	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem < 4*time.Second || rem > 6*time.Second {
		t.Fatalf("expected lockout ~5s, got %v", rem)
	}
}

func TestLockoutTracker_WithMaxLockout(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock), WithMaxLockout(3*time.Second))

	for i := 0; i < 10; i++ {
		lt.RecordFailure("10.0.0.1", "admin")
	}

	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem > 4*time.Second {
		t.Fatalf("expected lockout capped at 3s, got %v", rem)
	}
}

func TestLockoutTracker_WithClock(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	clock.Advance(500 * time.Millisecond)

	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected still locked at 500ms")
	}

	clock.Advance(600 * time.Millisecond)
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected unlocked after 1.1s")
	}
}

func TestAdminLoginLockoutsTotalMetric(t *testing.T) {
	lt := NewLockoutTracker()

	v1a := testutil.ToFloat64(AdminLoginLockoutsTotal.WithLabelValues("10.0.0.1", "admin"))
	v2a := testutil.ToFloat64(AdminLoginLockoutsTotal.WithLabelValues("10.0.0.2", "root"))

	lt.RecordFailure("10.0.0.1", "admin")
	lt.RecordFailure("10.0.0.1", "admin")
	lt.RecordFailure("10.0.0.2", "root")

	v1b := testutil.ToFloat64(AdminLoginLockoutsTotal.WithLabelValues("10.0.0.1", "admin"))
	v2b := testutil.ToFloat64(AdminLoginLockoutsTotal.WithLabelValues("10.0.0.2", "root"))

	if v1b-v1a != 2 {
		t.Fatalf("expected 2 increments for 10.0.0.1/admin, got %f", v1b-v1a)
	}
	if v2b-v2a != 1 {
		t.Fatalf("expected 1 increment for 10.0.0.2/root, got %f", v2b-v2a)
	}
}

func TestLockoutTracker_RaceCondition(t *testing.T) {
	lt := NewLockoutTracker()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lt.RecordFailure("10.0.0.1", "admin")
			_ = lt.IsLocked("10.0.0.1", "admin")
			_ = lt.LockoutRemaining("10.0.0.1", "admin")
			_ = lt.Attempts("10.0.0.1", "admin")
			if idx%2 == 0 {
				lt.Reset("10.0.0.1", "admin")
			}
		}(i)
	}
	wg.Wait()
}

func TestLockoutTracker_RecordFailureTwice(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	rem1 := lt.LockoutRemaining("10.0.0.1", "admin")

	clock.Advance(3 * time.Second)

	lt.RecordFailure("10.0.0.1", "admin")
	rem2 := lt.LockoutRemaining("10.0.0.1", "admin")

	if rem2 <= rem1 {
		t.Fatalf("expected lockout to increase after 2nd failure: first=%v second=%v", rem1, rem2)
	}
	if lt.Attempts("10.0.0.1", "admin") != 2 {
		t.Fatalf("expected 2 attempts, got %d", lt.Attempts("10.0.0.1", "admin"))
	}
}

func TestLockoutTracker_NoCleanupForActive(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")
	clock.Advance(10 * time.Second)
	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(500 * time.Millisecond)

	if !lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected 10.0.0.1/admin to still be locked")
	}

	clock.Advance(2 * time.Second)
	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected 10.0.0.1/admin to be unlocked after lockout expires")
	}
}

func TestLockoutTracker_ZeroDurationDefaults(t *testing.T) {
	lt := NewLockoutTracker()
	if lt.baseLockout != DefaultBaseLockout {
		t.Fatalf("expected base lockout %v, got %v", DefaultBaseLockout, lt.baseLockout)
	}
	if lt.maxLockout != DefaultMaxLockout {
		t.Fatalf("expected max lockout %v, got %v", DefaultMaxLockout, lt.maxLockout)
	}
}

func TestLockoutTracker_IsLockedAfterCleanupLazy(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(30 * time.Second)

	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected lockout expired")
	}

	if lt.IsLocked("10.0.0.1", "admin") {
		t.Fatal("expected still unlocked on second check")
	}
}

func TestLockoutTracker_LockoutRemainingAfterExpiry(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(5 * time.Second)

	rem := lt.LockoutRemaining("10.0.0.1", "admin")
	if rem != 0 {
		t.Fatalf("expected 0 remaining after expiry, got %v", rem)
	}
}

func TestLockoutTracker_CleanupExpiredEntriesAfterInterval(t *testing.T) {
	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	lt := NewLockoutTracker(WithClock(clock))

	lt.RecordFailure("10.0.0.1", "admin")

	clock.Advance(70 * time.Second)

	lt.RecordFailure("10.0.0.2", "root")

	if lt.Attempts("10.0.0.1", "admin") != 0 {
		t.Fatal("expected expired entry to be cleaned up during RecordFailure")
	}
}
