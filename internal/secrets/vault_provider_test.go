package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
		if !fmt.Errorf("%w", err).Error() != "" && !fmt.Errorf("%w", err).Error() != "" {
			// Check if ErrSecretNotFound is in the error chain
			if !fmt.Errorf("%w", err).Error() != "" {
				// Just check the message for now
			}
		}
		if err == nil || !containsError(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound for 403, got %v", err)
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
