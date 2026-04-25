package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClient_SuccessAfterRetries(t *testing.T) {
	var mu sync.Mutex
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		curr := attempts
		mu.Unlock()

		if curr < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewClient()
	client.MaxRetries = 3
	client.BaseBackoff = 10 * time.Millisecond
	client.MaxBackoff = 50 * time.Millisecond

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)

	if err != nil {
		t.Fatalf("expected success, got error %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	mu.Lock()
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	mu.Unlock()
}

func TestClient_MaxRetriesReached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient()
	client.MaxRetries = 2
	client.BaseBackoff = 5 * time.Millisecond
	client.MaxBackoff = 10 * time.Millisecond
	client.Breaker = NewCircuitBreaker(10, time.Second)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req)

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxRetriesReached) && !strings.Contains(err.Error(), "max retries reached") {
		t.Fatalf("expected ErrMaxRetriesReached, got %v", err)
	}
}

func TestClient_TimeoutAndContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient()
	client.RequestTimeout = 20 * time.Millisecond
	client.MaxRetries = 1
	client.BaseBackoff = 5 * time.Millisecond
	client.Breaker = NewCircuitBreaker(10, time.Second)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := client.Do(req)

	if err == nil {
		t.Fatalf("expected timeout error")
	}

	// External context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	_, err2 := client.Do(req2)
	if err2 == nil || !errors.Is(err2, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err2)
	}
}

func TestClient_CircuitBreakerOpens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient()
	client.MaxRetries = 0
	client.Breaker = NewCircuitBreaker(2, time.Second)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)

	// 1
	client.Do(req)
	// 2 (Opens breaker)
	client.Do(req)

	// 3 (Fast fails)
	_, err := client.Do(req)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestClient_NetworkError(t *testing.T) {
	client := NewClient()
	client.MaxRetries = 1
	client.BaseBackoff = 5 * time.Millisecond

	req, _ := http.NewRequest(http.MethodGet, "http://invalid.local.domain.which.does.not.exist", nil)
	_, err := client.Do(req)

	if err == nil {
		t.Fatalf("expected network error")
	}
}

func TestClient_CalculateBackoff(t *testing.T) {
	b1 := calculateBackoff(0, 100*time.Millisecond, 1*time.Second)
	if b1 < 100*time.Millisecond || b1 > 120*time.Millisecond {
		t.Fatalf("b1 out of range: %v", b1)
	}

	b2 := calculateBackoff(3, 100*time.Millisecond, 1*time.Second)
	if b2 < 800*time.Millisecond || b2 > 960*time.Millisecond {
		t.Fatalf("b2 out of range: %v", b2)
	}

	b3 := calculateBackoff(10, 100*time.Millisecond, 500*time.Millisecond)
	if b3 < 500*time.Millisecond || b3 > 600*time.Millisecond {
		t.Fatalf("b3 out of range: %v", b3)
	}
}

type dropRoundTripper struct{}

func (rt *dropRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(errReader{}),
	}, nil
}

type errReader struct{}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated partial read error")
}

func TestClient_PartialReads(t *testing.T) {
	client := NewClient()
	client.MaxRetries = 1
	client.BaseBackoff = 5 * time.Millisecond
	client.HTTPClient.Transport = &dropRoundTripper{}

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	resp, err := client.Do(req)

	if err != nil {
		t.Fatalf("Do should succeed to get headers... %v", err)
	}

	_, readErr := io.ReadAll(resp.Body)
	if readErr == nil || !strings.Contains(readErr.Error(), "simulated") {
		t.Fatalf("expected simulated error, got %v", readErr)
	}
}
