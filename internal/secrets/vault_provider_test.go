package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVaultProvider_GetSecret(t *testing.T) {
	t.Run("successful fetch and cache hit", func(t *testing.T) {
		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			if r.Header.Get("X-Vault-Token") != "test-token" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			resp := vaultResponse{}
			resp.Data.Data = map[string]interface{}{"TEST_KEY": "test-value"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "test-token", "secret/data")

		// First call - fetch from server
		val, err := p.GetSecret(context.Background(), "TEST_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "test-value" {
			t.Errorf("expected test-value, got %s", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("expected 1 call, got %d", callCount)
		}

		// Second call - cache hit
		val, err = p.GetSecret(context.Background(), "TEST_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "test-value" {
			t.Errorf("expected test-value, got %s", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("expected cache hit (still 1 call), got %d", callCount)
		}
	})

	t.Run("KV v2 envelope unwrapping", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// KV v2 structure: data: { data: { ... } }
			fmt.Fprint(w, `{"data": {"data": {"value": "unwrapped-value"}}}`)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")
		val, err := p.GetSecret(context.Background(), "SOME_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "unwrapped-value" {
			t.Errorf("expected unwrapped-value, got %s", val)
		}
	})

	t.Run("Vault 403 returns ErrSecretNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "bad-token", "secret/data")
		_, err := p.GetSecret(context.Background(), "KEY")
		if err == nil || !containsError(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound for 403, got %v", err)
		}
	})

	t.Run("Vault 401 is a hard error, not a fallthrough", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "revoked-token", "secret/data")
		_, err := p.GetSecret(context.Background(), "KEY")
		if err == nil {
			t.Fatal("expected an error for 401, got nil")
		}
		if errors.Is(err, ErrSecretNotFound) {
			t.Error("401 must not be treated as not-found; a revoked token is a hard failure")
		}
	})

	t.Run("Vault 5xx is a hard error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")
		_, err := p.GetSecret(context.Background(), "KEY")
		if err == nil || containsError(err, ErrSecretNotFound) {
			t.Errorf("expected a hard 500 error, got %v", err)
		}
	})

	t.Run("Network timeout returns ErrProviderTimeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")
		p.client.Timeout = 10 * time.Millisecond // Force client timeout

		_, err := p.GetSecret(context.Background(), "KEY")
		if err == nil || !containsError(err, ErrProviderTimeout) {
			t.Errorf("expected ErrProviderTimeout for timeout, got %v", err)
		}
	})

	t.Run("cancelled context returns ErrProviderTimeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		p := NewVaultProvider(server.URL, "token", "secret/data")
		_, err := p.GetSecret(ctx, "KEY")
		if err == nil || !containsError(err, ErrProviderTimeout) {
			t.Errorf("expected ErrProviderTimeout for cancelled context, got %v", err)
		}
	})

	t.Run("empty key returns ErrSecretNotFound", func(t *testing.T) {
		p := NewVaultProvider("http://127.0.0.1:1", "token", "secret/data")
		_, err := p.GetSecret(context.Background(), "   ")
		if err == nil || !containsError(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound for empty key, got %v", err)
		}
	})

	t.Run("background refresh on TTL expiry", func(t *testing.T) {
		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&callCount, 1)
			resp := vaultResponse{}
			resp.Data.Data = map[string]interface{}{"KEY": fmt.Sprintf("val-%d", count)}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")
		p.ttl = 100 * time.Millisecond

		// First fetch
		val, _ := p.GetSecret(context.Background(), "KEY")
		if val != "val-1" {
			t.Errorf("got %s", val)
		}

		// Wait until nearing expiry (but not yet expired)
		time.Sleep(85 * time.Millisecond)

		// This call should trigger background refresh
		val, _ = p.GetSecret(context.Background(), "KEY")
		if val != "val-1" {
			t.Errorf("should still get old value, got %s", val)
		}

		// Wait a bit for background refresh to complete
		time.Sleep(50 * time.Millisecond)

		// Next call should get new value
		val, _ = p.GetSecret(context.Background(), "KEY")
		if val != "val-2" {
			t.Errorf("expected val-2 after background refresh, got %s", val)
		}
	})

	t.Run("concurrent readers coalesce into a single request", func(t *testing.T) {
		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&callCount, 1)
			time.Sleep(20 * time.Millisecond) // widen the stampede window
			resp := vaultResponse{}
			resp.Data.Data = map[string]interface{}{"KEY": "shared"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")

		const workers = 16
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				val, err := p.GetSecret(context.Background(), "KEY")
				if err != nil {
					errs <- err
					return
				}
				if val != "shared" {
					errs <- fmt.Errorf("unexpected value: %q", val)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("concurrent read error: %v", err)
		}
		if got := atomic.LoadInt32(&callCount); got != 1 {
			t.Errorf("expected a single Vault request (single-flight), got %d", got)
		}
	})

	t.Run("failure is recoverable on a later read", func(t *testing.T) {
		var callCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&callCount, 1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			resp := vaultResponse{}
			resp.Data.Data = map[string]interface{}{"KEY": "after-recovery"}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewVaultProvider(server.URL, "token", "secret/data")

		if _, err := p.GetSecret(context.Background(), "KEY"); err == nil {
			t.Fatal("expected an error on the first fetch")
		}
		val, err := p.GetSecret(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error after recovery: %v", err)
		}
		if val != "after-recovery" {
			t.Errorf("expected after-recovery, got %s", val)
		}
	})
}

func TestChainWithVault(t *testing.T) {
	t.Run("Vault 403 falls through to env", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		os.Setenv("VAULT_ADDR", server.URL)
		os.Setenv("VAULT_TOKEN", "bad")
		os.Setenv("ENV_KEY", "env-value")
		defer os.Unsetenv("VAULT_ADDR")
		defer os.Unsetenv("VAULT_TOKEN")
		defer os.Unsetenv("ENV_KEY")

		p := NewDefaultProvider()
		val, err := p.GetSecret(context.Background(), "ENV_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "env-value" {
			t.Errorf("expected env-value, got %s", val)
		}
		if !strings.Contains(p.Name(), "vault") || !strings.Contains(p.Name(), "env") {
			t.Errorf("expected chain name to contain vault and env, got %s", p.Name())
		}
	})
}

func containsError(err, target error) bool {
	return errors.Is(err, target)
}
