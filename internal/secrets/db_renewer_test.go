package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDBRenewer_Defaults(t *testing.T) {
	r := NewDBRenewer("http://vault:8200/", "token", "database/creds/app", 0)
	require.NotNil(t, r)
	assert.Equal(t, "http://vault:8200", r.address)
	assert.Equal(t, "database/creds/app", r.rolePath)
	assert.Equal(t, 30*time.Second, r.renewBefore)
}

func TestDBRenewer_IssueAndRenew(t *testing.T) {
	var issueCount atomic.Int32
	var renewCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/database/creds/app":
			issueCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {"username": "v-app-abc", "password": "s3cret"},
				"lease_id": "database/creds/app/lease1",
				"lease_duration": 2
			}`))
		case "/v1/sys/leases/renew":
			renewCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"lease_id": "database/creds/app/lease1",
				"lease_duration": 2
			}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", 1*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case cred := <-r.Credentials():
		assert.Equal(t, "v-app-abc", cred.Username)
		assert.Equal(t, "s3cret", cred.Password)
		assert.Equal(t, "database/creds/app/lease1", cred.LeaseID)
		assert.False(t, cred.ExpiresAt.IsZero())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	// Wait for at least one renewal to occur (lease_duration=2s, renewBefore=1s).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if renewCount.Load() >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, renewCount.Load(), int32(1), "expected at least one renewal")
	assert.Equal(t, int32(1), issueCount.Load(), "credentials should only be issued once, then renewed")
}

func TestDBRenewer_IssueFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	case <-time.After(2 * time.Second):
		t.Fatal("expected hard-fail on issue failure")
	}

	// Credentials channel must be closed after hard-fail.
	_, ok := <-r.Credentials()
	assert.False(t, ok, "credentials channel should be closed after hard-fail")
}

func TestDBRenewer_RenewalFailureKeepsLeaseUntilExpiry(t *testing.T) {
	var issueCount atomic.Int32
	var renewCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/database/creds/app":
			issueCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {"username": "v-app-abc", "password": "s3cret"},
				"lease_id": "database/creds/app/lease1",
				"lease_duration": 1
			}`))
		case "/v1/sys/leases/renew":
			renewCount.Add(1)
			http.Error(w, "lease not found", http.StatusBadRequest)
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	// First credential arrives.
	select {
	case cred := <-r.Credentials():
		assert.Equal(t, "v-app-abc", cred.Username)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	// Renewal fails; the lease (1s) should be used until expiry, then hard-fail.
	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expired without renewal")
	case <-time.After(5 * time.Second):
		t.Fatal("expected hard-fail after lease expiry")
	}

	assert.GreaterOrEqual(t, renewCount.Load(), int32(1), "renewal should have been attempted")
}

func TestDBRenewer_StopClosesChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {"username": "u", "password": "p"},
			"lease_id": "l",
			"lease_duration": 3600
		}`))
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case <-r.Credentials():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	r.Stop()
	_, ok := <-r.Credentials()
	assert.False(t, ok, "credentials channel should be closed after Stop")
}

func TestDBRenewer_IssueMissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {}, "lease_id": "l", "lease_duration": 60}`))
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing username/password")
	case <-time.After(2 * time.Second):
		t.Fatal("expected hard-fail on missing fields")
	}
}

func TestDBRenewer_IssueInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode issue response")
	case <-time.After(2 * time.Second):
		t.Fatal("expected hard-fail on invalid json")
	}
}

func TestDBRenewer_IssueZeroLeaseDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"username": "u", "password": "p"}, "lease_id": "l", "lease_duration": 0}`))
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing lease_duration")
	case <-time.After(2 * time.Second):
		t.Fatal("expected hard-fail on zero lease duration")
	}
}

func TestDBRenewer_RenewInvalidJSON(t *testing.T) {
	var issueCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/database/creds/app":
			issueCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {"username": "u", "password": "p"},
				"lease_id": "l",
				"lease_duration": 1
			}`))
		case "/v1/sys/leases/renew":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not-json`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case <-r.Credentials():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	// Renewal fails (invalid JSON); lease (1s) expires, then hard-fail.
	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expired without renewal")
	case <-time.After(5 * time.Second):
		t.Fatal("expected hard-fail after lease expiry")
	}
}

func TestDBRenewer_RenewZeroLeaseDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/database/creds/app":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {"username": "u", "password": "p"},
				"lease_id": "l",
				"lease_duration": 1
			}`))
		case "/v1/sys/leases/renew":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"lease_id": "l", "lease_duration": 0}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case <-r.Credentials():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expired without renewal")
	case <-time.After(5 * time.Second):
		t.Fatal("expected hard-fail after lease expiry")
	}
}

func TestDBRenewer_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {"username": "u", "password": "p"},
			"lease_id": "l",
			"lease_duration": 3600
		}`))
	}))
	defer srv.Close()

	r := NewDBRenewer(srv.URL, "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)

	select {
	case <-r.Credentials():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first credential")
	}

	cancel()
	// Channel should eventually close.
	select {
	case _, ok := <-r.Credentials():
		assert.False(t, ok, "credentials channel should close on context cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("credentials channel did not close after context cancellation")
	}
}

func TestDBRenewer_IssueRequestError(t *testing.T) {
	// Point at an unreachable address so the HTTP request fails.
	r := NewDBRenewer("http://127.0.0.1:1", "token", "database/creds/app", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	select {
	case err := <-r.Errors():
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vault issue request failed")
	case <-time.After(3 * time.Second):
		t.Fatal("expected hard-fail on request error")
	}
}

func TestDBRenewer_JSONMarshal(t *testing.T) {
	// Ensure DBCredential is JSON-serializable (used in tests/logging).
	cred := DBCredential{Username: "u", Password: "p", LeaseID: "l", ExpiresAt: time.Now()}
	b, err := json.Marshal(cred)
	require.NoError(t, err)
	assert.Contains(t, string(b), "u")
}
