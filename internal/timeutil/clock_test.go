package timeutil

import (
	"testing"
	"time"
)

func TestSystemClock_ReturnsCurrentTimeInUTC(t *testing.T) {
	before := time.Now().UTC()
	got := SystemClock.Now()
	after := time.Now().UTC()

	if got.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %v", got.Location())
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("SystemClock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestFakeClock_SetAndNow(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	next := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)
	c.Set(next)
	if got := c.Now(); !got.Equal(next) {
		t.Fatalf("after Set, Now() = %v, want %v", got, next)
	}
}

func TestFakeClock_SetNormalizesToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	local := time.Date(2026, 1, 1, 8, 0, 0, 0, loc)
	c := NewFakeClock(local)

	got := c.Now()
	if got.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %v", got.Location())
	}
	if !got.Equal(local) {
		t.Fatalf("Now() = %v, want same instant as %v", got, local)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)

	c.Advance(90 * time.Minute)
	want := start.Add(90 * time.Minute)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(+90m), Now() = %v, want %v", got, want)
	}

	c.Advance(-30 * time.Minute)
	want = want.Add(-30 * time.Minute)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(-30m), Now() = %v, want %v", got, want)
	}
}

// TestFakeClock_AdvanceAcrossSpringForward verifies that Advance operates on
// the absolute instant, so it does not lose or gain time across a
// daylight-saving "spring forward" transition, even when the FakeClock was
// seeded from a local (non-UTC) time.Time.
func TestFakeClock_AdvanceAcrossSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 2024-03-10 01:30 EST is 30 minutes before the 02:00 -> 03:00 jump.
	before := time.Date(2024, 3, 10, 1, 30, 0, 0, loc)
	c := NewFakeClock(before)

	c.Advance(2 * time.Hour)

	wantUTC := before.UTC().Add(2 * time.Hour)
	got := c.Now()
	if !got.Equal(wantUTC) {
		t.Fatalf("Advance across spring-forward: got %v, want %v (absolute instant)", got, wantUTC)
	}

	// The wall-clock gap means local time reads 04:30, not 03:30: the clock
	// skipped the nonexistent 02:00-03:00 hour, confirming no manual
	// component-based arithmetic snuck in.
	gotLocal := got.In(loc)
	wantLocal := time.Date(2024, 3, 10, 4, 30, 0, 0, loc)
	if !gotLocal.Equal(wantLocal) {
		t.Fatalf("local reading after spring-forward = %v, want %v", gotLocal, wantLocal)
	}
}

// TestFakeClock_AdvanceAcrossFallBack verifies correct absolute-instant
// arithmetic across a daylight-saving "fall back" transition, where a local
// wall-clock hour repeats.
func TestFakeClock_AdvanceAcrossFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 2024-11-03 00:30 EDT is 90 minutes before the 02:00 -> 01:00 fall-back.
	before := time.Date(2024, 11, 3, 0, 30, 0, 0, loc)
	c := NewFakeClock(before)

	c.Advance(2 * time.Hour)

	wantUTC := before.UTC().Add(2 * time.Hour)
	got := c.Now()
	if !got.Equal(wantUTC) {
		t.Fatalf("Advance across fall-back: got %v, want %v (absolute instant)", got, wantUTC)
	}

	// The repeated hour means local time reads 01:30 (EST, the second time
	// through 01:00-02:00), i.e. only 1 wall-clock hour appears to have
	// passed even though a full 2 hours elapsed in absolute terms.
	gotLocal := got.In(loc)
	wantLocal := time.Date(2024, 11, 3, 1, 30, 0, 0, loc)
	if !gotLocal.Equal(wantLocal) {
		t.Fatalf("local reading after fall-back = %v, want %v", gotLocal, wantLocal)
	}
}
