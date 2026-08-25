package secrets

import (
	"bytes"
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

// DBCredential holds short-lived Postgres credentials issued by Vault's
// database secrets engine. The credential is only valid until ExpiresAt.
type DBCredential struct {
	Username  string
	Password  string
	LeaseID   string
	ExpiresAt time.Time
}

// DBRenewer issues short-lived Postgres credentials from Vault's database
// secrets engine and pushes them onto a channel. It renews the lease before
// expiry; if renewal fails it keeps the existing credential until expiry,
// then hard-fails (the credentials channel is closed and the error is
// surfaced on the Errors channel).
//
// This reduces the blast radius of a leaked credential to the TTL window
// configured on the Vault role, and rotates credentials without requiring a
// pod restart.
type DBRenewer struct {
	address     string
	token       string
	rolePath    string
	client      *http.Client
	renewBefore time.Duration

	creds    chan DBCredential
	errCh    chan error
	stop     chan struct{}
	stopOnce sync.Once
}

// NewDBRenewer creates a background task that issues credentials from the
// Vault database secrets engine at the given role path (e.g.
// "database/creds/app"). renewBefore is how long before expiry the lease is
// renewed; it must be positive and less than the role's TTL.
func NewDBRenewer(address, token, rolePath string, renewBefore time.Duration) *DBRenewer {
	if renewBefore <= 0 {
		renewBefore = 30 * time.Second
	}
	return &DBRenewer{
		address:     strings.TrimSuffix(address, "/"),
		token:       token,
		rolePath:    strings.Trim(rolePath, "/"),
		client:      &http.Client{Timeout: 5 * time.Second},
		renewBefore: renewBefore,
		creds:       make(chan DBCredential, 1),
		errCh:       make(chan error, 1),
		stop:        make(chan struct{}),
	}
}

// Credentials returns the channel on which newly issued/rotated credentials
// are delivered. The channel is closed when the renewer stops or hard-fails.
func (r *DBRenewer) Credentials() <-chan DBCredential {
	return r.creds
}

// Errors returns a channel that receives a terminal error when the renewer
// hard-fails (i.e. the lease expired without a successful renewal).
func (r *DBRenewer) Errors() <-chan error {
	return r.errCh
}

// Start begins the background renewal loop. It issues the first credential
// immediately and then renews before each expiry.
func (r *DBRenewer) Start(ctx context.Context) {
	go r.run(ctx)
}

// Stop terminates the renewal loop and closes the credentials channel.
func (r *DBRenewer) Stop() {
	r.stopOnce.Do(func() {
		close(r.stop)
	})
}

func (r *DBRenewer) run(ctx context.Context) {
	defer close(r.creds)

	cred, err := r.issue(ctx)
	if err != nil {
		r.fail(err)
		return
	}
	r.push(cred)

	for {
		// Wait until it's time to renew (renewBefore before expiry).
		wait := time.Until(cred.ExpiresAt) - r.renewBefore
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-r.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		timer.Stop()

		// Try to renew the lease. Renewal only extends the lease; the
		// username/password are unchanged, so carry them over.
		renewed, err := r.renew(ctx, cred.LeaseID)
		if err == nil {
			renewed.Username = cred.Username
			renewed.Password = cred.Password
			cred = renewed
			r.push(cred)
			continue
		}

		// Renewal failed. Keep the existing credential until it expires.
		// If the credential is already expired, hard-fail immediately.
		if time.Now().After(cred.ExpiresAt) {
			r.fail(fmt.Errorf("db credential lease expired without renewal: %w", err))
			return
		}

		// Wait until expiry, then hard-fail.
		expiryTimer := time.NewTimer(time.Until(cred.ExpiresAt))
		select {
		case <-ctx.Done():
			expiryTimer.Stop()
			return
		case <-r.stop:
			expiryTimer.Stop()
			return
		case <-expiryTimer.C:
		}
		expiryTimer.Stop()
		r.fail(fmt.Errorf("db credential lease expired without renewal: %w", err))
		return
	}
}

func (r *DBRenewer) push(cred DBCredential) {
	select {
	case r.creds <- cred:
	case <-r.stop:
	}
}

func (r *DBRenewer) fail(err error) {
	select {
	case r.errCh <- err:
	default:
	}
}

// issue requests a fresh set of credentials from Vault's database secrets
// engine: POST /v1/{rolePath}/creds/{role}.
func (r *DBRenewer) issue(ctx context.Context) (DBCredential, error) {
	url := fmt.Sprintf("%s/v1/%s", r.address, r.rolePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return DBCredential{}, fmt.Errorf("create issue request: %w", err)
	}
	if r.token != "" {
		req.Header.Set("X-Vault-Token", r.token)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return DBCredential{}, fmt.Errorf("vault issue request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DBCredential{}, fmt.Errorf("vault issue returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DBCredential{}, fmt.Errorf("read issue response: %w", err)
	}

	var out struct {
		Data struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"data"`
		LeaseID       string `json:"lease_id"`
		LeaseDuration int    `json:"lease_duration"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return DBCredential{}, fmt.Errorf("decode issue response: %w", err)
	}
	if out.Data.Username == "" || out.Data.Password == "" {
		return DBCredential{}, errors.New("vault issue response missing username/password")
	}
	if out.LeaseDuration <= 0 {
		return DBCredential{}, errors.New("vault issue response missing lease_duration")
	}

	return DBCredential{
		Username:  out.Data.Username,
		Password:  out.Data.Password,
		LeaseID:   out.LeaseID,
		ExpiresAt: time.Now().Add(time.Duration(out.LeaseDuration) * time.Second),
	}, nil
}

// renew extends the lease for the given lease ID via
// PUT /v1/sys/leases/renew. It returns a credential with the updated expiry.
func (r *DBRenewer) renew(ctx context.Context, leaseID string) (DBCredential, error) {
	url := fmt.Sprintf("%s/v1/sys/leases/renew", r.address)
	payload, err := json.Marshal(map[string]interface{}{
		"lease_id": leaseID,
	})
	if err != nil {
		return DBCredential{}, fmt.Errorf("marshal renew payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return DBCredential{}, fmt.Errorf("create renew request: %w", err)
	}
	if r.token != "" {
		req.Header.Set("X-Vault-Token", r.token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return DBCredential{}, fmt.Errorf("vault renew request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DBCredential{}, fmt.Errorf("vault renew returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DBCredential{}, fmt.Errorf("read renew response: %w", err)
	}

	var out struct {
		LeaseID       string `json:"lease_id"`
		LeaseDuration int    `json:"lease_duration"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return DBCredential{}, fmt.Errorf("decode renew response: %w", err)
	}
	if out.LeaseDuration <= 0 {
		return DBCredential{}, errors.New("vault renew response missing lease_duration")
	}

	return DBCredential{
		Username:  "", // username/password are unchanged by renewal
		Password:  "",
		LeaseID:   out.LeaseID,
		ExpiresAt: time.Now().Add(time.Duration(out.LeaseDuration) * time.Second),
	}, nil
}
