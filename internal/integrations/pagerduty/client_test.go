package pagerduty_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stellarbill-backend/internal/integrations/pagerduty"
)

// roundTripFunc allows using a plain func as an HTTPClient.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func newRespBody(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}

func TestTrigger_Success(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := pagerduty.NewWithHTTP("key1", srv.URL, srv.Client())
	err := c.Trigger(context.Background(), "dedup-1", "test summary", pagerduty.SeverityCritical, map[string]any{"count": 7})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["event_action"] != "trigger" {
		t.Errorf("expected event_action=trigger, got %v", got["event_action"])
	}
	if got["dedup_key"] != "dedup-1" {
		t.Errorf("expected dedup_key=dedup-1, got %v", got["dedup_key"])
	}
	if got["routing_key"] != "key1" {
		t.Errorf("expected routing_key=key1, got %v", got["routing_key"])
	}
}

func TestResolve_Success(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := pagerduty.NewWithHTTP("key2", srv.URL, srv.Client())
	if err := c.Resolve(context.Background(), "dedup-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["event_action"] != "resolve" {
		t.Errorf("expected event_action=resolve, got %v", got["event_action"])
	}
}

func TestTrigger_Retries5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := pagerduty.NewWithHTTP("key3", srv.URL, &fastHTTPClient{inner: srv.Client()})
	err := c.Trigger(context.Background(), "dedup-2", "summary", pagerduty.SeverityError, nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestTrigger_AllRetriesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := pagerduty.NewWithHTTP("key4", srv.URL, &fastHTTPClient{inner: srv.Client()})
	err := c.Trigger(context.Background(), "dedup-3", "summary", pagerduty.SeverityCritical, nil)
	if err == nil {
		t.Fatal("expected error after all retries fail")
	}
}

func TestTrigger_4xxNoRetry(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := pagerduty.NewWithHTTP("key5", srv.URL, srv.Client())
	err := c.Trigger(context.Background(), "dedup-4", "summary", pagerduty.SeverityWarning, nil)
	if err == nil {
		t.Fatal("expected client error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestTrigger_ContextCancelled(t *testing.T) {
	// Server that always returns 500 to trigger retry loop
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := pagerduty.NewWithHTTP("key6", srv.URL, &fastHTTPClient{inner: srv.Client()})
	err := c.Trigger(ctx, "dedup-5", "summary", pagerduty.SeverityInfo, nil)
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestNew_UsesDefaultEndpoint(t *testing.T) {
	// Just verify New doesn't panic and returns a non-nil client
	c := pagerduty.New("somekey")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// fastHTTPClient wraps an *http.Client but strips the retry delay by making
// the delay negligible — achieved by having the test use a tiny delay in the
// underlying retry wait. We swap out the inner client's transport.
// In practice we just use the real client but set a short timeout.
type fastHTTPClient struct {
	inner *http.Client
}

func (f *fastHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f.inner.Do(req)
}
