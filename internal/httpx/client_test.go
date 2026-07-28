package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sony/gobreaker"
)

// fakeResolver lets tests control DNS answers and count lookups without
// touching the real network.
type fakeResolver struct {
	mu    sync.Mutex
	addrs []string
	calls int
	err   error
}

func (f *fakeResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.addrs...), nil
}

func (f *fakeResolver) setAddrs(addrs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addrs = addrs
}

func (f *fakeResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeClock gives tests direct control over DNS TTL expiry.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func loopbackIP(t *testing.T, server *httptest.Server) string {
	t.Helper()
	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split server address: %v", err)
	}
	return host
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.DialTimeout = 2 * time.Second
	cfg.RequestTimeout = 2 * time.Second
	cfg.CircuitOpenTimeout = time.Minute // stays open for the test's duration
	return cfg
}

func TestDo_SucceedsAndReusesConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resolver := &fakeResolver{addrs: []string{loopbackIP(t, server)}}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	pool := New(cfg)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	host := req.URL.Hostname()

	resp, err := pool.Do(req)
	if err != nil {
		t.Fatalf("first Do: %v", err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()
	if got := testutil.ToFloat64(connReuseRatio.WithLabelValues(host)); got != 0 {
		t.Fatalf("expected reuse ratio 0 after first request, got %v", got)
	}

	// Give the transport's read loop a moment to return the just-closed
	// connection to the idle pool before the next request looks for one.
	time.Sleep(50 * time.Millisecond)

	req2, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp2, err := pool.Do(req2)
	if err != nil {
		t.Fatalf("second Do: %v", err)
	}
	resp2.Body.Close()
	if got := testutil.ToFloat64(connReuseRatio.WithLabelValues(host)); got != 0.5 {
		t.Fatalf("expected reuse ratio 0.5 after second (reused) request, got %v", got)
	}
}

func TestDo_MissingHost(t *testing.T) {
	pool := New(testConfig())
	req, _ := http.NewRequest(http.MethodGet, "http://[::1]", nil)
	req.URL.Host = ""

	if _, err := pool.Do(req); err == nil {
		t.Fatal("expected error for request with no host")
	}
}

func TestDo_PerHostConnectionBudget(t *testing.T) {
	const maxConns = 2
	const totalRequests = 6

	var inFlight int32
	var maxObserved int32
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxObserved)
			if cur <= old || atomic.CompareAndSwapInt32(&maxObserved, old, cur) {
				break
			}
		}
		<-release
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resolver := &fakeResolver{addrs: []string{loopbackIP(t, server)}}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	cfg.MaxConnsPerHost = maxConns
	cfg.RequestTimeout = 10 * time.Second
	pool := New(cfg)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			resp, err := pool.Do(req)
			if err != nil {
				t.Errorf("Do: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}

	// Give every goroutine a chance to queue up against the transport
	// before releasing the handler.
	time.Sleep(200 * time.Millisecond)
	close(release)
	wg.Wait()

	if atomic.LoadInt32(&maxObserved) > maxConns {
		t.Fatalf("observed %d concurrent connections, want <= %d", maxObserved, maxConns)
	}
}

func TestDo_CircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	// Nothing is listening on this loopback port, so every dial fails fast.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close() // now guaranteed refused

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	resolver := &fakeResolver{addrs: []string{host}}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	cfg.CircuitMaxFailures = 2
	cfg.DialTimeout = time.Second
	cfg.RequestTimeout = time.Second
	pool := New(cfg)

	url := "http://" + net.JoinHostPort("failhost.invalid", port) + "/"

	var lastErr error
	for i := 0; i < int(cfg.CircuitMaxFailures); i++ {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		_, lastErr = pool.Do(req)
		if lastErr == nil {
			t.Fatalf("attempt %d: expected connection failure, got nil error", i)
		}
		if errors.Is(lastErr, ErrCircuitOpen) {
			t.Fatalf("attempt %d: circuit opened too early: %v", i, lastErr)
		}
	}

	if state := pool.State("failhost.invalid"); state != gobreaker.StateOpen {
		t.Fatalf("expected breaker to be open after %d consecutive failures, got %v", cfg.CircuitMaxFailures, state)
	}

	callsBeforeTrip := resolver.callCount()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	_, err = pool.Do(req)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if resolver.callCount() != callsBeforeTrip {
		t.Fatal("expected an open breaker to reject the request without touching DNS/network")
	}
}

func TestPool_StateUnknownHostIsClosed(t *testing.T) {
	pool := New(testConfig())
	if state := pool.State("never-seen.invalid"); state != gobreaker.StateClosed {
		t.Fatalf("expected unseen host to report StateClosed, got %v", state)
	}
}

func TestResolveHost_DNSTTLZeroRefreshesEveryCall(t *testing.T) {
	resolver := &fakeResolver{addrs: []string{"10.0.0.1"}}
	clock := newFakeClock()
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = clock
	cfg.DNSTTL = 0
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	for i := 1; i <= 3; i++ {
		ip, _, err := pool.resolveHost(context.Background(), "example.test", entry)
		if err != nil {
			t.Fatalf("resolveHost call %d: %v", i, err)
		}
		if ip != "10.0.0.1" {
			t.Fatalf("call %d: got ip %q, want 10.0.0.1", i, ip)
		}
		if got := resolver.callCount(); got != i {
			t.Fatalf("after call %d: resolver invoked %d times, want %d (TTL=0 must refresh every request)", i, got, i)
		}
	}
}

func TestResolveHost_CachesWithinTTL(t *testing.T) {
	resolver := &fakeResolver{addrs: []string{"10.0.0.1"}}
	clock := newFakeClock()
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = clock
	cfg.DNSTTL = 30 * time.Second
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	if _, _, err := pool.resolveHost(context.Background(), "example.test", entry); err != nil {
		t.Fatalf("first resolveHost: %v", err)
	}
	clock.Advance(10 * time.Second)
	ip, changed, err := pool.resolveHost(context.Background(), "example.test", entry)
	if err != nil {
		t.Fatalf("second resolveHost: %v", err)
	}
	if changed {
		t.Fatal("expected no change while within TTL")
	}
	if ip != "10.0.0.1" {
		t.Fatalf("got ip %q, want 10.0.0.1", ip)
	}
	if got := resolver.callCount(); got != 1 {
		t.Fatalf("resolver invoked %d times within TTL window, want 1", got)
	}
}

func TestResolveHost_RefreshesAndReportsChangeAfterTTLExpires(t *testing.T) {
	resolver := &fakeResolver{addrs: []string{"10.0.0.1"}}
	clock := newFakeClock()
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = clock
	cfg.DNSTTL = 30 * time.Second
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	if _, _, err := pool.resolveHost(context.Background(), "example.test", entry); err != nil {
		t.Fatalf("first resolveHost: %v", err)
	}

	resolver.setAddrs("10.0.0.2")
	clock.Advance(31 * time.Second)

	ip, changed, err := pool.resolveHost(context.Background(), "example.test", entry)
	if err != nil {
		t.Fatalf("second resolveHost: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true once the A record moved after TTL expiry")
	}
	if ip != "10.0.0.2" {
		t.Fatalf("got ip %q, want 10.0.0.2", ip)
	}
	if got := resolver.callCount(); got != 2 {
		t.Fatalf("resolver invoked %d times, want 2", got)
	}
}

func TestResolveHost_SameAddressAfterExpiryIsNotAChange(t *testing.T) {
	resolver := &fakeResolver{addrs: []string{"10.0.0.1"}}
	clock := newFakeClock()
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = clock
	cfg.DNSTTL = 30 * time.Second
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	if _, _, err := pool.resolveHost(context.Background(), "example.test", entry); err != nil {
		t.Fatalf("first resolveHost: %v", err)
	}
	clock.Advance(31 * time.Second)

	_, changed, err := pool.resolveHost(context.Background(), "example.test", entry)
	if err != nil {
		t.Fatalf("second resolveHost: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when the refreshed address is identical")
	}
}

func TestResolveHost_ResolverErrorIsReported(t *testing.T) {
	resolver := &fakeResolver{err: errors.New("boom")}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	_, _, err := pool.resolveHost(context.Background(), "example.test", entry)
	if err == nil {
		t.Fatal("expected an error from a failing resolver")
	}
}

func TestDialContext_FallsBackToDefaultDialerOnResolverError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resolver := &fakeResolver{err: errors.New("dns unavailable")}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	pool := New(cfg)

	// The request targets the server's real loopback address directly, so
	// even though our resolver always errors, the fallback dialer's own
	// resolution of the literal IP address should still succeed.
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := pool.Do(req)
	if err != nil {
		t.Fatalf("expected fallback dial to succeed despite resolver error: %v", err)
	}
	resp.Body.Close()
}

func TestConfig_DefaultsAreApplied(t *testing.T) {
	pool := New(Config{})
	if pool.cfg.MaxConnsPerHost != DefaultConfig().MaxConnsPerHost {
		t.Fatalf("expected default MaxConnsPerHost, got %d", pool.cfg.MaxConnsPerHost)
	}
	if pool.cfg.Resolver == nil {
		t.Fatal("expected a default resolver")
	}
	if pool.cfg.Clock == nil {
		t.Fatal("expected a default clock")
	}
}

func TestConfig_ZeroDNSTTLIsPreserved(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DNSTTL = 0
	pool := New(cfg)
	if pool.cfg.DNSTTL != 0 {
		t.Fatalf("expected explicit zero DNSTTL to survive setDefaults, got %v", pool.cfg.DNSTTL)
	}
}

func TestPool_ClientReturnsHostScopedClient(t *testing.T) {
	pool := New(testConfig())
	a := pool.Client("a.example.test")
	b := pool.Client("b.example.test")
	if a == b {
		t.Fatal("expected distinct clients per host")
	}
	if pool.Client("a.example.test") != a {
		t.Fatal("expected the same client to be returned for repeated calls on the same host")
	}
}

func TestPool_CloseIdleConnections(t *testing.T) {
	pool := New(testConfig())
	_ = pool.Client("a.example.test")
	_ = pool.Client("b.example.test")
	pool.CloseIdleConnections() // must not panic with multiple hosts registered
}

func TestResolveHost_EmptyAddressesWithoutErrorIsReported(t *testing.T) {
	resolver := &fakeResolver{addrs: []string{}}
	cfg := testConfig()
	cfg.Resolver = resolver
	cfg.Clock = newFakeClock()
	pool := New(cfg)
	entry := pool.hostEntry("example.test")

	_, _, err := pool.resolveHost(context.Background(), "example.test", entry)
	if err == nil {
		t.Fatal("expected an error when the resolver returns zero addresses")
	}
}

func TestDialContext_MalformedAddrFallsBackToDialer(t *testing.T) {
	cfg := testConfig()
	cfg.Resolver = &fakeResolver{addrs: []string{"127.0.0.1"}}
	cfg.Clock = newFakeClock()
	pool := New(cfg)
	entry := pool.hostEntry("example.test")
	dialer := &net.Dialer{Timeout: time.Second}

	dial := pool.dialContext("example.test", entry, dialer)
	// "example.test" has no port, so net.SplitHostPort fails and dialContext
	// must fall back to the plain dialer instead of panicking on the split.
	if _, err := dial(context.Background(), "tcp", "example.test"); err == nil {
		t.Fatal("expected the fallback dialer to fail against a portless, unresolvable address")
	}
}

func TestRealClockAdvances(t *testing.T) {
	c := realClock{}
	start := c.Now()
	time.Sleep(time.Millisecond)
	if !c.Now().After(start) {
		t.Fatal("expected the real clock to advance")
	}
}
