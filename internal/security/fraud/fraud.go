// Package fraud provides an aggregate, tenant-scoped detector for abusive
// request patterns such as credential stuffing, enumeration of subscription
// IDs, and rapid plan churn.
//
// The collector maintains per-tenant sliding-window counters for a small set
// of fraud signals. When a signal crosses its configured threshold within the
// observation window, a structured "fraud.signal.detected" audit event is
// emitted via the configured Emitter.
//
// Privacy: the collector never accepts or stores raw PII or credentials. Only
// opaque tenant scope keys and event counts cross its boundary, and tenant
// identifiers are hashed before they appear in any emitted event.
package fraud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Signal identifies a category of suspicious behavior tracked per tenant.
type Signal string

const (
	// SignalAuthFailRate counts authentication failures (credential stuffing).
	SignalAuthFailRate Signal = "auth_fail_rate"
	// SignalSubscriptionIDMisses counts lookups for subscription IDs that do
	// not belong to the tenant (enumeration probing).
	SignalSubscriptionIDMisses Signal = "subscription_id_misses"
	// SignalPlanChurnRate counts plan changes (rapid upgrade/downgrade churn).
	SignalPlanChurnRate Signal = "plan_churn_rate"
)

// AuditAction is the canonical action recorded when a signal trips.
const AuditAction = "fraud.signal.detected"

// AuditEvent is the structured payload emitted when a threshold is crossed.
// It is intentionally free of raw identifiers: TenantHash is a keyed hash of
// the tenant scope, never the scope itself.
type AuditEvent struct {
	Action     string    `json:"action"`
	Signal     Signal    `json:"signal"`
	TenantHash string    `json:"tenant_hash"`
	Count      int64     `json:"count"`
	Threshold  int64     `json:"threshold"`
	Window     string    `json:"window"`
	DetectedAt time.Time `json:"detected_at"`
}

// Emitter receives fraud events. It is satisfied by an adapter over the
// project's audit logger; see Adapt in emitter.go.
type Emitter interface {
	Emit(e AuditEvent) error
}

// SignalConfig configures one signal's sliding window and threshold.
type SignalConfig struct {
	// Window is the span over which events are counted.
	Window time.Duration
	// Buckets is the number of sub-windows; more buckets give finer roll-over.
	Buckets int
	// Threshold is the count within Window at which the signal is considered
	// tripped. A threshold <= 0 disables detection for the signal (events are
	// still counted but never emitted).
	Threshold int64
	// Cooldown suppresses repeat emissions for the same tenant+signal until it
	// elapses, preventing event storms while a tenant stays over threshold.
	Cooldown time.Duration
}

// Config configures the Collector.
type Config struct {
	// Signals maps each tracked Signal to its window/threshold settings.
	// Signals absent from the map are not tracked.
	Signals map[Signal]SignalConfig
	// HashSecret keys the tenant-hash HMAC so emitted hashes are not trivially
	// reversible. If empty a process-local random-ish default is used.
	HashSecret string
	// IdleTTL controls how long an empty tenant entry is retained before it is
	// eligible for eviction. Defaults to the longest configured window.
	IdleTTL time.Duration
}

// DefaultConfig returns a sensible production-leaning configuration covering
// all three signals.
func DefaultConfig() Config {
	return Config{
		Signals: map[Signal]SignalConfig{
			SignalAuthFailRate: {
				Window:    time.Minute,
				Buckets:   12,
				Threshold: 20,
				Cooldown:  time.Minute,
			},
			SignalSubscriptionIDMisses: {
				Window:    5 * time.Minute,
				Buckets:   10,
				Threshold: 30,
				Cooldown:  2 * time.Minute,
			},
			SignalPlanChurnRate: {
				Window:    time.Hour,
				Buckets:   12,
				Threshold: 6,
				Cooldown:  10 * time.Minute,
			},
		},
		HashSecret: "stellabill-fraud-default",
	}
}

// signalState bundles a sliding window with its last-emission instant.
type signalState struct {
	window     *slidingWindow
	lastEmit   time.Time
	lastActive time.Time
}

// tenantState holds every tracked signal's state for a single tenant.
type tenantState struct {
	mu      sync.Mutex
	signals map[Signal]*signalState
}

// Collector aggregates fraud signals across tenants and emits audit events
// when thresholds trip. It is safe for concurrent use.
type Collector struct {
	cfg     Config
	emitter Emitter
	clock   Clock
	secret  []byte
	idleTTL time.Duration

	mu      sync.RWMutex
	tenants map[string]*tenantState
}

// Option customizes Collector construction.
type Option func(*Collector)

// WithClock overrides the time source (primarily for tests).
func WithClock(c Clock) Option {
	return func(col *Collector) {
		if c != nil {
			col.clock = c
		}
	}
}

// NewCollector builds a Collector. emitter may be nil, in which case detections
// are counted but not published (useful for shadow mode).
func NewCollector(cfg Config, emitter Emitter, opts ...Option) *Collector {
	if cfg.Signals == nil {
		cfg.Signals = DefaultConfig().Signals
	}
	secret := cfg.HashSecret
	if secret == "" {
		secret = "stellabill-fraud-default"
	}

	idle := cfg.IdleTTL
	if idle <= 0 {
		for _, sc := range cfg.Signals {
			if sc.Window > idle {
				idle = sc.Window
			}
		}
		if idle <= 0 {
			idle = time.Minute
		}
	}

	c := &Collector{
		cfg:     cfg,
		emitter: emitter,
		clock:   systemClock{},
		secret:  []byte(secret),
		idleTTL: idle,
		tenants: make(map[string]*tenantState),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Observe records a single occurrence of signal for tenant and, if the signal
// is now over threshold (and not within cooldown), emits an audit event.
//
// tenant is an opaque scope key (e.g. a tenant ID). It is never logged in the
// clear; only its keyed hash appears in emitted events. Empty tenant keys and
// untracked signals are ignored.
//
// It returns true when an event was emitted on this call.
func (c *Collector) Observe(tenant string, signal Signal) bool {
	return c.observeN(tenant, signal, 1)
}

func (c *Collector) observeN(tenant string, signal Signal, n int64) bool {
	if tenant == "" || n <= 0 {
		return false
	}
	sc, tracked := c.cfg.Signals[signal]
	if !tracked {
		return false
	}

	now := c.clock.Now().UTC()
	ts := c.tenantStateFor(tenant)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	st := ts.signals[signal]
	if st == nil {
		st = &signalState{window: newSlidingWindow(sc.Window, sc.Buckets)}
		ts.signals[signal] = st
	}
	st.lastActive = now

	count := st.window.add(now, n)

	if sc.Threshold <= 0 || count < sc.Threshold {
		return false
	}
	// Cooldown: suppress repeat emissions while the tenant stays hot.
	if !st.lastEmit.IsZero() && sc.Cooldown > 0 && now.Sub(st.lastEmit) < sc.Cooldown {
		return false
	}
	st.lastEmit = now

	if c.emitter == nil {
		return false
	}
	evt := AuditEvent{
		Action:     AuditAction,
		Signal:     signal,
		TenantHash: c.hashTenant(tenant),
		Count:      count,
		Threshold:  sc.Threshold,
		Window:     sc.Window.String(),
		DetectedAt: now,
	}
	// Emit best-effort; a sink failure must not break the request path.
	_ = c.emitter.Emit(evt)
	return true
}

// Count returns the current windowed count for tenant+signal without recording
// a new event. Returns 0 for unknown tenants or untracked signals.
func (c *Collector) Count(tenant string, signal Signal) int64 {
	if tenant == "" {
		return 0
	}
	if _, tracked := c.cfg.Signals[signal]; !tracked {
		return 0
	}
	c.mu.RLock()
	ts := c.tenants[tenant]
	c.mu.RUnlock()
	if ts == nil {
		return 0
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	st := ts.signals[signal]
	if st == nil {
		return 0
	}
	return st.window.count(c.clock.Now().UTC())
}

// tenantStateFor returns (creating if needed) the state container for tenant.
func (c *Collector) tenantStateFor(tenant string) *tenantState {
	c.mu.RLock()
	ts := c.tenants[tenant]
	c.mu.RUnlock()
	if ts != nil {
		return ts
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if ts = c.tenants[tenant]; ts != nil {
		return ts
	}
	ts = &tenantState{signals: make(map[Signal]*signalState)}
	c.tenants[tenant] = ts
	return ts
}

// EvictIdle removes tenant entries whose every signal window is empty and whose
// last activity predates IdleTTL. Returns the number of tenants evicted. Call
// periodically from a background goroutine to bound memory.
func (c *Collector) EvictIdle() int {
	now := c.clock.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()

	var evicted int
	for tenant, ts := range c.tenants {
		ts.mu.Lock()
		idle := true
		for _, st := range ts.signals {
			if now.Sub(st.lastActive) < c.idleTTL || !st.window.empty(now) {
				idle = false
				break
			}
		}
		ts.mu.Unlock()
		if idle {
			delete(c.tenants, tenant)
			evicted++
		}
	}
	return evicted
}

// hashTenant returns a keyed, hex-encoded HMAC of the tenant scope so emitted
// events can correlate the same tenant without revealing its identity.
func (c *Collector) hashTenant(tenant string) string {
	h := hmac.New(sha256.New, c.secret)
	h.Write([]byte(tenant))
	return hex.EncodeToString(h.Sum(nil))
}
