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

type VaultProvider struct {
	address    string
	token      string
	pathPrefix string
	client     *http.Client

	cache map[string]*cacheEntry
	mu    sync.RWMutex
	ttl   time.Duration
}

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

func (p *VaultProvider) GetSecret(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty key: %w", ErrSecretNotFound)
	}

	p.mu.RLock()
	entry, ok := p.cache[key]
	p.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
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

	data := vResp.Data.Data
	if val, ok := data[key]; ok {
		return fmt.Sprint(val), nil
	}
	if val, ok := data["value"]; ok {
		return fmt.Sprint(val), nil
	}

	return "", fmt.Errorf("key %q not found in vault data: %w", key, ErrSecretNotFound)
}

func (p *VaultProvider) Metadata(ctx context.Context, key string) (SecretMetadata, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return SecretMetadata{}, ErrMetadataNotFound
	}

	url := fmt.Sprintf("%s/v1/%s%s", p.address, p.pathPrefix, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("failed to create metadata request: %w", err)
	}

	if p.token != "" {
		req.Header.Set("X-Vault-Token", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("vault metadata request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SecretMetadata{}, ErrMetadataNotFound
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("failed to read metadata response body: %w", err)
	}

	var vResp vaultResponse
	if err := json.Unmarshal(body, &vResp); err != nil {
		return SecretMetadata{}, fmt.Errorf("failed to decode vault metadata response: %w", err)
	}

	data := vResp.Data.Data
	md := SecretMetadata{
		Name:     key,
		Source:   p.Name(),
		Owner:    fmt.Sprint(data["owner"]),
		Required: true,
	}

	if v, ok := data["rotation_cadence"].(string); ok {
		md.RotationCadence = v
		if d, err := ParseDurationLikeRotation(v); err == nil {
			md.RotationInterval = d
		}
	}
	if v, ok := data["last_rotated_at"].(string); ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			md.LastRotatedAt = t
		}
	}
	if !md.LastRotatedAt.IsZero() && md.RotationInterval > 0 {
		md.NextRotationDueAt = md.LastRotatedAt.Add(md.RotationInterval)
	}
	if steps, ok := data["verification_steps"].([]interface{}); ok {
		for _, s := range steps {
			md.VerificationSteps = append(md.VerificationSteps, fmt.Sprint(s))
		}
	}

	return md, nil
}

func (p *VaultProvider) refreshSecret(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = p.fetchAndCache(ctx, key)
}

func (p *VaultProvider) Name() string {
	return "vault"
}

func osIsTimeout(err error) bool {
	type timeout interface {
		Timeout() bool
	}
	t, ok := err.(timeout)
	return ok && t.Timeout()
}