// Package httpx provides a shared HTTP client pool for outbound requests.
//
// Each remote host gets its own *http.Transport (giving it a hard,
// independent connection budget via MaxConnsPerHost), its own circuit
// breaker (so one unhealthy upstream cannot exhaust connection slots meant
// for others), and its own DNS cache entry that is refreshed on a
// per-host TTL. When a host's resolved address changes, that host's idle
// connections are recycled so future requests do not keep talking to a
// decommissioned IP behind a stale A record.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/sony/gobreaker"
)

// ErrCircuitOpen is returned by Do when the target host's circuit breaker
// is open (or in the limited half-open probing state) and the request was
// rejected without touching the network.
var ErrCircuitOpen = errors.New("httpx: circuit breaker open")

// Clock abstracts time.Now so DNS TTL expiry can be driven deterministically
// in tests instead of waiting on a real clock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Resolver resolves a hostname to a list of addresses. *net.Resolver
// satisfies this interface, which is all Pool requires by default.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Config controls the per-host tunables applied to every host the Pool
// serves. All fields have sane defaults; use DefaultConfig as a base.
type Config struct {
	// MaxConnsPerHost caps the number of simultaneous connections (in
	// flight or idle) a single host may hold open. This is the "per-host
	// budget" that keeps one noisy upstream from starving the others.
	MaxConnsPerHost int
	// MaxIdleConnsPerHost caps idle (keep-alive) connections kept open per
	// host between requests.
	MaxIdleConnsPerHost int
	// IdleConnTimeout closes idle connections older than this duration.
	IdleConnTimeout time.Duration
	// DialTimeout bounds how long a single TCP dial may take.
	DialTimeout time.Duration
	// RequestTimeout bounds an entire request/response round trip.
	RequestTimeout time.Duration

	// DNSTTL controls how long a resolved address is trusted before the
	// pool re-resolves the host. A zero value re-resolves on every dial,
	// which is the safest (if least efficient) setting for hosts behind
	// aggressive DNS failover.
	DNSTTL time.Duration

	// CircuitMaxFailures is the number of consecutive transport-level
	// failures (timeouts, connection errors, DNS errors) that trip a
	// host's breaker open.
	CircuitMaxFailures uint32
	// CircuitOpenTimeout is how long the breaker stays open before
	// allowing a single half-open probe request through.
	CircuitOpenTimeout time.Duration
	// CircuitHalfOpenMax is the number of probe requests allowed through
	// while the breaker is half-open.
	CircuitHalfOpenMax uint32

	// Resolver performs DNS lookups. Defaults to net.DefaultResolver.
	Resolver Resolver
	// Clock supplies the current time for DNS TTL bookkeeping. Defaults
	// to the real wall clock.
	Clock Clock
}

// DefaultConfig returns reasonable production defaults.
func DefaultConfig() Config {
	return Config{
		MaxConnsPerHost:     64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		DialTimeout:         5 * time.Second,
		RequestTimeout:      10 * time.Second,
		DNSTTL:              30 * time.Second,
		CircuitMaxFailures:  5,
		CircuitOpenTimeout:  30 * time.Second,
		CircuitHalfOpenMax:  1,
		Resolver:            net.DefaultResolver,
		Clock:               realClock{},
	}
}

func (c *Config) setDefaults() {
	d := DefaultConfig()
	if c.MaxConnsPerHost <= 0 {
		c.MaxConnsPerHost = d.MaxConnsPerHost
	}
	if c.MaxIdleConnsPerHost <= 0 {
		c.MaxIdleConnsPerHost = d.MaxIdleConnsPerHost
	}
	if c.IdleConnTimeout <= 0 {
		c.IdleConnTimeout = d.IdleConnTimeout
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = d.DialTimeout
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = d.RequestTimeout
	}
	// DNSTTL is intentionally allowed to be zero (refresh every request).
	if c.CircuitMaxFailures == 0 {
		c.CircuitMaxFailures = d.CircuitMaxFailures
	}
	if c.CircuitOpenTimeout <= 0 {
		c.CircuitOpenTimeout = d.CircuitOpenTimeout
	}
	if c.CircuitHalfOpenMax == 0 {
		c.CircuitHalfOpenMax = d.CircuitHalfOpenMax
	}
	if c.Resolver == nil {
		c.Resolver = d.Resolver
	}
	if c.Clock == nil {
		c.Clock = d.Clock
	}
}

// Pool is a shared, per-host-budgeted HTTP client. It is safe for
// concurrent use and is intended to be constructed once and reused across
// all outbound integrations (webhooks, PagerDuty, Slack, etc).
type Pool struct {
	cfg Config

	mu    sync.Mutex
	hosts map[string]*hostEntry
}

// New creates a Pool. Zero-valued fields in cfg fall back to DefaultConfig.
func New(cfg Config) *Pool {
	cfg.setDefaults()
	return &Pool{
		cfg:   cfg,
		hosts: make(map[string]*hostEntry),
	}
}

// hostEntry bundles everything the pool tracks for a single remote host.
type hostEntry struct {
	host      string
	client    *http.Client
	transport *http.Transport
	breaker   *gobreaker.CircuitBreaker

	dnsMu         sync.Mutex
	dnsAddrs      []string
	dnsResolvedAt time.Time

	reuseMu     sync.Mutex
	reusedCount uint64
	totalCount  uint64
}

// Do executes req against the shared pool for req.URL's host: it is routed
// through that host's dedicated transport (enforcing the per-host
// connection budget), through that host's circuit breaker, and dials using
// a TTL-refreshed DNS lookup so stale A records get evicted.
//
// The returned error wraps ErrCircuitOpen when the host's breaker rejected
// the request without hitting the network.
func (p *Pool) Do(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if host == "" {
		return nil, fmt.Errorf("httpx: request URL has no host: %s", req.URL)
	}
	entry := p.hostEntry(host)

	var reused bool
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	result, err := entry.breaker.Execute(func() (interface{}, error) {
		return entry.client.Do(req)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, fmt.Errorf("%w: host=%s", ErrCircuitOpen, host)
		}
		return nil, err
	}

	entry.recordConnReuse(reused)
	return result.(*http.Response), nil
}

// Client returns an *http.Client scoped to host, sharing that host's
// pooled transport. Prefer Do when possible; Client exists for callers
// that need to hand an *http.Client to third-party code.
func (p *Pool) Client(host string) *http.Client {
	return p.hostEntry(host).client
}

// State reports the current circuit breaker state for host. Hosts that
// have never been dialed report gobreaker.StateClosed.
func (p *Pool) State(host string) gobreaker.State {
	p.mu.Lock()
	entry, ok := p.hosts[host]
	p.mu.Unlock()
	if !ok {
		return gobreaker.StateClosed
	}
	return entry.breaker.State()
}

// CloseIdleConnections closes idle connections across every host the pool
// has served. Safe to call during graceful shutdown.
func (p *Pool) CloseIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.hosts {
		entry.transport.CloseIdleConnections()
	}
}

func (p *Pool) hostEntry(host string) *hostEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.hosts[host]; ok {
		return entry
	}
	entry := p.newHostEntry(host)
	p.hosts[host] = entry
	return entry
}

func (p *Pool) newHostEntry(host string) *hostEntry {
	entry := &hostEntry{host: host}

	dialer := &net.Dialer{Timeout: p.cfg.DialTimeout}
	transport := &http.Transport{
		MaxConnsPerHost:     p.cfg.MaxConnsPerHost,
		MaxIdleConnsPerHost: p.cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     p.cfg.IdleConnTimeout,
		DialContext:         p.dialContext(host, entry, dialer),
	}
	entry.transport = transport
	entry.client = &http.Client{
		Transport: transport,
		Timeout:   p.cfg.RequestTimeout,
	}
	entry.breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        host,
		MaxRequests: p.cfg.CircuitHalfOpenMax,
		Timeout:     p.cfg.CircuitOpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= p.cfg.CircuitMaxFailures
		},
		IsSuccessful: func(err error) bool {
			return err == nil
		},
	})
	return entry
}

// dialContext returns a DialContext that resolves host through the pool's
// Resolver (subject to DNSTTL caching) and dials the resolved address
// directly, so the pool controls exactly which IP a connection targets. If
// resolution fails, it falls back to the standard dialer's own resolution
// of addr so a transient resolver hiccup doesn't take the host down
// outright.
func (p *Pool) dialContext(host string, entry *hostEntry, dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return dialer.DialContext(ctx, network, addr)
		}

		ip, changed, err := p.resolveHost(ctx, host, entry)
		if err != nil {
			return dialer.DialContext(ctx, network, addr)
		}
		if changed {
			// The A record moved out from under us; drop idle connections
			// pointed at the old IP so they don't keep serving stale
			// traffic.
			entry.transport.CloseIdleConnections()
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
	}
}

// resolveHost returns the current address for host, refreshing it from the
// resolver when the cached entry has exceeded DNSTTL (or DNSTTL is zero).
// changed reports whether the primary address differs from the previously
// cached one, signaling that pooled connections should be recycled.
func (p *Pool) resolveHost(ctx context.Context, host string, entry *hostEntry) (ip string, changed bool, err error) {
	now := p.cfg.Clock.Now()

	entry.dnsMu.Lock()
	fresh := p.cfg.DNSTTL > 0 && len(entry.dnsAddrs) > 0 && now.Sub(entry.dnsResolvedAt) < p.cfg.DNSTTL
	if fresh {
		ip = entry.dnsAddrs[0]
		entry.dnsMu.Unlock()
		return ip, false, nil
	}
	entry.dnsMu.Unlock()

	addrs, err := p.cfg.Resolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		if err == nil {
			err = fmt.Errorf("httpx: no addresses found for host %s", host)
		}
		return "", false, err
	}

	entry.dnsMu.Lock()
	defer entry.dnsMu.Unlock()
	changed = len(entry.dnsAddrs) > 0 && entry.dnsAddrs[0] != addrs[0]
	entry.dnsAddrs = addrs
	entry.dnsResolvedAt = now
	return addrs[0], changed, nil
}

// recordConnReuse updates the host's connection-reuse counters and
// publishes the resulting ratio to the http_client_conn_reuse_ratio gauge.
func (e *hostEntry) recordConnReuse(reused bool) {
	e.reuseMu.Lock()
	e.totalCount++
	if reused {
		e.reusedCount++
	}
	ratio := float64(e.reusedCount) / float64(e.totalCount)
	e.reuseMu.Unlock()

	connReuseRatio.WithLabelValues(e.host).Set(ratio)
}
