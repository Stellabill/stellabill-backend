package fraud

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockClock is a controllable Clock for deterministic tests.
type mockClock struct {
	mu  sync.Mutex
	now time.Time
}

func newMockClock(t time.Time) *mockClock { return &mockClock{now: t.UTC()} }

func (m *mockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *mockClock) advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}

func (m *mockClock) set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = t.UTC()
}

// captureEmitter records emitted events and can be made to fail.
type captureEmitter struct {
	mu     sync.Mutex
	events []AuditEvent
	fail   bool
}

func (c *captureEmitter) Emit(e AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("sink down")
	}
	c.events = append(c.events, e)
	return nil
}

func (c *captureEmitter) all() []AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AuditEvent, len(c.events))
	copy(out, c.events)
	return out
}

func testConfig() Config {
	return Config{
		Signals: map[Signal]SignalConfig{
			SignalAuthFailRate: {Window: time.Minute, Buckets: 6, Threshold: 3, Cooldown: time.Minute},
		},
		HashSecret: "test-secret",
	}
}

func TestObserve_TripsAtThreshold(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	if c.Observe("tenant-a", SignalAuthFailRate) {
		t.Fatal("emitted on first observe")
	}
	if c.Observe("tenant-a", SignalAuthFailRate) {
		t.Fatal("emitted on second observe")
	}
	if !c.Observe("tenant-a", SignalAuthFailRate) {
		t.Fatal("expected emit on third observe (threshold=3)")
	}

	evts := em.all()
	if len(evts) != 1 {
		t.Fatalf("got %d events, want 1", len(evts))
	}
	e := evts[0]
	if e.Action != AuditAction || e.Signal != SignalAuthFailRate {
		t.Fatalf("unexpected event: %+v", e)
	}
	if e.Count != 3 || e.Threshold != 3 {
		t.Fatalf("count/threshold = %d/%d, want 3/3", e.Count, e.Threshold)
	}
	if e.TenantHash == "" || e.TenantHash == "tenant-a" {
		t.Fatalf("tenant hash leaks identity or empty: %q", e.TenantHash)
	}
}

func TestObserve_Cooldown(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	for i := 0; i < 3; i++ {
		c.Observe("t", SignalAuthFailRate)
	}
	if n := len(em.all()); n != 1 {
		t.Fatalf("after threshold got %d events, want 1", n)
	}
	// Still within cooldown: further observations must not re-emit.
	clk.advance(10 * time.Second)
	c.Observe("t", SignalAuthFailRate)
	if n := len(em.all()); n != 1 {
		t.Fatalf("during cooldown got %d events, want 1", n)
	}
	// Past cooldown and still over threshold: re-emits.
	clk.advance(time.Minute)
	c.Observe("t", SignalAuthFailRate)
	c.Observe("t", SignalAuthFailRate)
	c.Observe("t", SignalAuthFailRate)
	if n := len(em.all()); n != 2 {
		t.Fatalf("after cooldown got %d events, want 2", n)
	}
}

func TestObserve_WindowRollOverResetsCount(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	c.Observe("t", SignalAuthFailRate)
	c.Observe("t", SignalAuthFailRate)
	if got := c.Count("t", SignalAuthFailRate); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	// Roll the whole window forward; old events expire and we stay under
	// threshold, so no emission.
	clk.advance(2 * time.Minute)
	if got := c.Count("t", SignalAuthFailRate); got != 0 {
		t.Fatalf("count after roll = %d, want 0", got)
	}
	if c.Observe("t", SignalAuthFailRate) {
		t.Fatal("should not emit after window reset")
	}
}

func TestObserve_HotTenantBurst(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	cfg := Config{
		Signals: map[Signal]SignalConfig{
			SignalAuthFailRate: {Window: time.Minute, Buckets: 12, Threshold: 50, Cooldown: time.Hour},
		},
		HashSecret: "s",
	}
	c := NewCollector(cfg, em, WithClock(clk))

	emits := 0
	for i := 0; i < 500; i++ {
		if c.Observe("hot", SignalAuthFailRate) {
			emits++
		}
		clk.advance(10 * time.Millisecond)
	}
	// A long cooldown means a single burst yields exactly one emission.
	if emits != 1 {
		t.Fatalf("hot burst emitted %d times, want 1", emits)
	}
	if got := c.Count("hot", SignalAuthFailRate); got < 50 {
		t.Fatalf("hot tenant count = %d, want >= threshold", got)
	}
}

func TestObserve_ClockSkewTolerance(t *testing.T) {
	// Simulate clock skew: time jumps backward then forward. The window must
	// not panic and must still attribute counts to the correct sub-windows.
	clk := newMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	c.Observe("t", SignalAuthFailRate)
	// Clock skews backward by 5s (NTP correction).
	clk.advance(-5 * time.Second)
	c.Observe("t", SignalAuthFailRate)
	// Then forward again.
	clk.advance(10 * time.Second)
	got := c.Observe("t", SignalAuthFailRate)
	if !got {
		t.Fatal("expected threshold trip despite clock skew")
	}
	if c := c.Count("t", SignalAuthFailRate); c < 3 {
		t.Fatalf("count under skew = %d, want >= 3", c)
	}
}

func TestObserve_NonLocalTimezoneNormalized(t *testing.T) {
	// A clock returning a non-UTC instant must produce the same window math.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tz data unavailable: %v", err)
	}
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	clk.set(time.Date(2026, 6, 1, 9, 30, 0, 0, loc))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))
	for i := 0; i < 3; i++ {
		c.Observe("t", SignalAuthFailRate)
	}
	if n := len(em.all()); n != 1 {
		t.Fatalf("got %d events, want 1", n)
	}
}

func TestObserve_TenantIsolation(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	c.Observe("a", SignalAuthFailRate)
	c.Observe("a", SignalAuthFailRate)
	c.Observe("b", SignalAuthFailRate)
	if got := c.Count("a", SignalAuthFailRate); got != 2 {
		t.Fatalf("tenant a count = %d, want 2", got)
	}
	if got := c.Count("b", SignalAuthFailRate); got != 1 {
		t.Fatalf("tenant b count = %d, want 1", got)
	}
	if len(em.all()) != 0 {
		t.Fatal("no tenant should have tripped yet")
	}
}

func TestObserve_IgnoredInputs(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	c := NewCollector(testConfig(), em, WithClock(clk))

	if c.Observe("", SignalAuthFailRate) {
		t.Fatal("empty tenant should be ignored")
	}
	if c.Observe("t", Signal("unknown")) {
		t.Fatal("untracked signal should be ignored")
	}
	if c.observeN("t", SignalAuthFailRate, 0) {
		t.Fatal("non-positive n should be ignored")
	}
	if got := c.Count("", SignalAuthFailRate); got != 0 {
		t.Fatalf("count empty tenant = %d, want 0", got)
	}
	if got := c.Count("t", Signal("unknown")); got != 0 {
		t.Fatalf("count untracked = %d, want 0", got)
	}
	if got := c.Count("never-seen", SignalAuthFailRate); got != 0 {
		t.Fatalf("count unknown tenant = %d, want 0", got)
	}
}

func TestCount_TenantSeenButSignalNeverObserved(t *testing.T) {
	// A tenant whose state exists for one signal returns 0 for a different,
	// tracked-but-unobserved signal (exercises the st==nil branch in Count).
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := Config{Signals: map[Signal]SignalConfig{
		SignalAuthFailRate:         {Window: time.Minute, Buckets: 6, Threshold: 3},
		SignalSubscriptionIDMisses: {Window: time.Minute, Buckets: 6, Threshold: 3},
	}}
	c := NewCollector(cfg, &captureEmitter{}, WithClock(clk))
	c.Observe("t", SignalAuthFailRate)
	if got := c.Count("t", SignalSubscriptionIDMisses); got != 0 {
		t.Fatalf("count = %d, want 0 for unobserved signal", got)
	}
}

func TestObserve_ThresholdDisabled(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	cfg := Config{
		Signals: map[Signal]SignalConfig{
			SignalAuthFailRate: {Window: time.Minute, Buckets: 6, Threshold: 0},
		},
		HashSecret: "s",
	}
	c := NewCollector(cfg, em, WithClock(clk))
	for i := 0; i < 100; i++ {
		c.Observe("t", SignalAuthFailRate)
	}
	if len(em.all()) != 0 {
		t.Fatal("threshold<=0 should never emit")
	}
	if got := c.Count("t", SignalAuthFailRate); got != 100 {
		t.Fatalf("count = %d, want 100 (still counted)", got)
	}
}

func TestObserve_ShadowModeNilEmitter(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := NewCollector(testConfig(), nil, WithClock(clk))
	for i := 0; i < 5; i++ {
		if c.Observe("t", SignalAuthFailRate) {
			t.Fatal("nil emitter must report no emission")
		}
	}
	if got := c.Count("t", SignalAuthFailRate); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
}

func TestObserve_EmitterFailureIsSwallowed(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{fail: true}
	c := NewCollector(testConfig(), em, WithClock(clk))
	// Threshold trips; emitter returns error but Observe must still report the
	// detection and must not panic.
	c.Observe("t", SignalAuthFailRate)
	c.Observe("t", SignalAuthFailRate)
	if !c.Observe("t", SignalAuthFailRate) {
		t.Fatal("expected detection reported even when sink fails")
	}
}

func TestEvictIdle(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	em := &captureEmitter{}
	cfg := testConfig()
	cfg.IdleTTL = time.Minute
	c := NewCollector(cfg, em, WithClock(clk))

	c.Observe("t", SignalAuthFailRate)
	// Not yet idle.
	if n := c.EvictIdle(); n != 0 {
		t.Fatalf("evicted %d, want 0 (still active)", n)
	}
	// Advance past window + idle TTL so the window is empty and lastActive old.
	clk.advance(3 * time.Minute)
	if n := c.EvictIdle(); n != 1 {
		t.Fatalf("evicted %d, want 1", n)
	}
	// Second pass evicts nothing.
	if n := c.EvictIdle(); n != 0 {
		t.Fatalf("evicted %d on empty, want 0", n)
	}
}

func TestEvictIdle_KeepsActiveTenant(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.IdleTTL = time.Hour
	c := NewCollector(cfg, &captureEmitter{}, WithClock(clk))
	c.Observe("t", SignalAuthFailRate)
	clk.advance(2 * time.Minute) // window empty but within IdleTTL
	if n := c.EvictIdle(); n != 0 {
		t.Fatalf("evicted %d, want 0 (within idle TTL)", n)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	for _, s := range []Signal{SignalAuthFailRate, SignalSubscriptionIDMisses, SignalPlanChurnRate} {
		sc, ok := cfg.Signals[s]
		if !ok {
			t.Fatalf("default config missing signal %s", s)
		}
		if sc.Window <= 0 || sc.Threshold <= 0 {
			t.Fatalf("signal %s has invalid defaults: %+v", s, sc)
		}
	}
}

func TestNewCollector_Defaults(t *testing.T) {
	// Empty config should fall back to DefaultConfig signals and a default
	// secret and a derived idle TTL.
	c := NewCollector(Config{}, nil)
	if len(c.cfg.Signals) == 0 {
		t.Fatal("expected default signals")
	}
	if len(c.secret) == 0 {
		t.Fatal("expected default secret")
	}
	if c.idleTTL <= 0 {
		t.Fatal("expected positive idle TTL")
	}
	// nil option is tolerated.
	_ = NewCollector(Config{}, nil, WithClock(nil))
}

func TestNewCollector_IdleTTLFallbackMinute(t *testing.T) {
	// Signals present but all with non-positive windows -> idle defaults to 1m.
	cfg := Config{Signals: map[Signal]SignalConfig{
		SignalAuthFailRate: {Window: 0, Buckets: 1, Threshold: 1},
	}}
	c := NewCollector(cfg, nil)
	if c.idleTTL != time.Minute {
		t.Fatalf("idleTTL = %v, want 1m fallback", c.idleTTL)
	}
}

func TestHashTenant_StableAndKeyed(t *testing.T) {
	c1 := NewCollector(Config{HashSecret: "k1"}, nil)
	c2 := NewCollector(Config{HashSecret: "k2"}, nil)
	h1a := c1.hashTenant("tenant")
	h1b := c1.hashTenant("tenant")
	if h1a != h1b {
		t.Fatal("hash not stable for same key+input")
	}
	if h1a == c2.hashTenant("tenant") {
		t.Fatal("different secrets produced identical hashes")
	}
	if strings.Contains(h1a, "tenant") {
		t.Fatal("hash leaks raw tenant id")
	}
}

func TestConvenienceObservers(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := DefaultConfig()
	em := &captureEmitter{}
	c := NewCollector(cfg, em, WithClock(clk))

	c.ObserveAuthFailure("t")
	c.ObserveSubscriptionIDMiss("t")
	c.ObservePlanChange("t")

	if got := c.Count("t", SignalAuthFailRate); got != 1 {
		t.Fatalf("auth fail count = %d, want 1", got)
	}
	if got := c.Count("t", SignalSubscriptionIDMisses); got != 1 {
		t.Fatalf("sub miss count = %d, want 1", got)
	}
	if got := c.Count("t", SignalPlanChurnRate); got != 1 {
		t.Fatalf("plan churn count = %d, want 1", got)
	}
}

func TestConcurrentObserve(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := Config{Signals: map[Signal]SignalConfig{
		SignalAuthFailRate: {Window: time.Hour, Buckets: 12, Threshold: 1 << 30, Cooldown: time.Hour},
	}}
	c := NewCollector(cfg, &captureEmitter{}, WithClock(clk))

	const goroutines, per = 20, 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tenant := fmt.Sprintf("tenant-%d", id%4)
			for i := 0; i < per; i++ {
				c.Observe(tenant, SignalAuthFailRate)
			}
		}(g)
	}
	wg.Wait()

	var total int64
	for i := 0; i < 4; i++ {
		total += c.Count(fmt.Sprintf("tenant-%d", i), SignalAuthFailRate)
	}
	if want := int64(goroutines * per); total != want {
		t.Fatalf("concurrent total = %d, want %d", total, want)
	}
}
