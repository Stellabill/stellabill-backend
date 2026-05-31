package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type vaultResponse struct {
	Data struct {
		Data map[string]interface{} `json:"data"`
	} `json:"data"`
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// VaultProvider implements the Provider interface for HashiCorp Vault.
type VaultProvider struct {
	address    string
	token      string
	pathPrefix string
	client     *http.Client
	
	cache map[string]*cacheEntry
	mu    sync.RWMutex
	ttl   time.Duration
}

// NewVaultProvider creates a new Vault provider.
func NewVaultProvider(address, token, pathPrefix string) *VaultProvider {
	if !strings.HasSuffix(pathPrefix, "/") && pathPrefix != "" {
		pathPrefix += "/"
	}
	return &VaultProvider{
		address:    strings.TrimSuffix(address, "/"),
		token:      token,
		pathPrefix: pathPrefix,
		client:     &http.Client{Timeout: 5 * time.Second},
		cache:      make(map[string]*cacheEntry),
		ttl:        5 * time.Minute,
	}
}

// GetSecret retrieves a secret from Vault KV v2.
func (p *VaultProvider) GetSecret(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty key: %w", ErrSecretNotFound)
	}

	// Check cache
	p.mu.RLock()
	entry, ok := p.cache[key]
	p.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		// Proactive background refresh if nearing expiry (last 20% of TTL)
		if time.Until(entry.expiresAt) < p.ttl/5 {
			go p.refreshSecret(key)
		}
		return entry.value, nil
	}

	return p.fetchAndCache(ctx, key)
}

func (p *VaultProvider) fetchAndCache(ctx context.Context, key string) (string, error) {
	val, err := p.fetchFromVault(ctx, key)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.cache[key] = &cacheEntry{
		value:     val,
		expiresAt: time.Now().Add(p.ttl),
	}
	p.mu.Unlock()

	return val, nil
}

func (p *VaultProvider) fetchFromVault(ctx context.Context, key string) (string, error) {
	url := fmt.Sprintf("%s/v1/%s%s", p.address, p.pathPrefix, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if p.token != "" {
		req.Header.Set("X-Vault-Token", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || osIsTimeout(err) {
			return "", ErrProviderTimeout
		}
		return "", fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Vault 403 falls through to next provider
		return "", fmt.Errorf("vault access forbidden: %w", ErrSecretNotFound)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("vault path not found: %w", ErrSecretNotFound)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var vResp vaultResponse
	if err := json.Unmarshal(body, &vResp); err != nil {
		return "", fmt.Errorf("failed to decode vault response: %w", err)
	}

	// KV v2 unwrapping: data.data[key] or data.data["value"]
	data := vResp.Data.Data
	if val, ok := data[key]; ok {
		return fmt.Sprint(val), nil
	}
	if val, ok := data["value"]; ok {
		return fmt.Sprint(val), nil
	}

	return "", fmt.Errorf("key %q not found in vault data: %w", key, ErrSecretNotFound)
}

func (p *VaultProvider) refreshSecret(key string) {
	// Use a background context with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = p.fetchAndCache(ctx, key)
}

func (p *VaultProvider) Name() string {
	return "vault"
}

// Helper to check for network timeouts
func osIsTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	t, ok := err.(timeout)
	return ok && t.Timeout()
}
