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

// callState models a single in-flight Vault fetch shared by concurrent
// callers. It implements single-flight so a stampede of readers (either cache
// misses or proactive refreshes near TTL expiry) coalesces into one HTTP
// request instead of N duplicate requests to the Vault cluster.
type callState struct {
	done chan struct{} // closed when the shared fetch completes
	val  string
	err  error
}

type VaultProvider struct {
	address    string
	token      string
	pathPrefix string
	client     *http.Client

	cache    map[string]*cacheEntry
	inflight map[string]*callState
	mu       sync.RWMutex
	ttl      time.Duration
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
		inflight:   make(map[string]*callState),
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
		// TTL caching with proactive refresh: as an entry nears expiry we
		// kick a background refresh while still serving the current value.
		// The single-flight guard in fetchAndCache keeps concurrent readers
		// from issuing a thundering herd of refresh requests.
		if time.Until(entry.expiresAt) < p.ttl/5 {
			go p.refreshSecret(key)
		}
		return entry.value, nil
	}

	return p.fetchAndCache(ctx, key)
}

func (p *VaultProvider) fetchAndCache(ctx context.Context, key string) (string, error) {
	// Single-flight: if another goroutine is already fetching this key, join
	// it and reuse its result instead of issuing a duplicate Vault request.
	p.mu.Lock()
	if cs, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		select {
		case <-cs.done:
			return cs.val, cs.err
		case <-ctx.Done():
			return "", fmt.Errorf("%w: %v", ErrProviderTimeout, ctx.Err())
		}
	}
	cs := &callState{done: make(chan struct{})}
	p.inflight[key] = cs
	p.mu.Unlock()

	val, err := p.fetchFromVault(ctx, key)

	p.mu.Lock()
	delete(p.inflight, key)
	if err == nil {
		p.cache[key] = &cacheEntry{
			value:     val,
			expiresAt: time.Now().Add(p.ttl),
		}
	}
	cs.val, cs.err = val, err
	close(cs.done)
	p.mu.Unlock()

	return val, err
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
		if errors.Is(err, context.DeadlineExceeded) || osIsTimeout(err) || ctx.Err() != nil {
			return "", ErrProviderTimeout
		}
		return "", fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	// 403 "permission denied" and 404 "no such secret" mean the secret is not
	// resolvable from this provider, so the chain falls through to the next
	// provider. Everything else is treated as a hard failure so a broken or
	// revoked token (401) or a degraded Vault backend (5xx) is never silently
	// masked by an environment fallback.
	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("vault access forbidden: %w", ErrSecretNotFound)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("vault path not found: %w", ErrSecretNotFound)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("vault access unauthorized: token rejected")
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
