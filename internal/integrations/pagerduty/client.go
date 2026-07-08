// Package pagerduty provides a minimal PagerDuty Events v2 client for
// triggering and resolving incidents.
package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultEndpoint = "https://events.pagerduty.com/v2/enqueue"
	maxRetries      = 3
	retryDelay      = time.Second
)

// Severity maps to PagerDuty event severity levels.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityError    Severity = "error"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// payload is the PagerDuty Events v2 request body.
type payload struct {
	RoutingKey  string   `json:"routing_key"`
	EventAction string   `json:"event_action"` // "trigger" or "resolve"
	DedupKey    string   `json:"dedup_key"`
	Payload     *details `json:"payload,omitempty"`
}

type details struct {
	Summary   string                 `json:"summary"`
	Source    string                 `json:"source"`
	Severity  Severity               `json:"severity"`
	Timestamp string                 `json:"timestamp"`
	CustomDetails map[string]any     `json:"custom_details,omitempty"`
}

// HTTPClient is the interface used for sending events, allowing test injection.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client sends PagerDuty Events v2 alerts.
type Client struct {
	routingKey string
	endpoint   string
	http       HTTPClient
}

// New creates a Client. routingKey must be a non-empty PagerDuty integration key.
func New(routingKey string) *Client {
	return &Client{
		routingKey: routingKey,
		endpoint:   defaultEndpoint,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithHTTP creates a Client using the provided HTTPClient and endpoint
// (useful for tests).
func NewWithHTTP(routingKey, endpoint string, hc HTTPClient) *Client {
	return &Client{
		routingKey: routingKey,
		endpoint:   endpoint,
		http:       hc,
	}
}

// Trigger fires a PagerDuty "trigger" event. dedupKey must be stable across
// restarts so PagerDuty can de-duplicate the incident.
func (c *Client) Trigger(ctx context.Context, dedupKey, summary string, sev Severity, customDetails map[string]any) error {
	p := payload{
		RoutingKey:  c.routingKey,
		EventAction: "trigger",
		DedupKey:    dedupKey,
		Payload: &details{
			Summary:       summary,
			Source:        "stellabill-backend",
			Severity:      sev,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			CustomDetails: customDetails,
		},
	}
	return c.send(ctx, p)
}

// Resolve fires a PagerDuty "resolve" event for the given dedupKey.
func (c *Client) Resolve(ctx context.Context, dedupKey string) error {
	p := payload{
		RoutingKey:  c.routingKey,
		EventAction: "resolve",
		DedupKey:    dedupKey,
	}
	return c.send(ctx, p)
}

// send POSTs the payload with exponential-like retry on 5xx responses.
func (c *Client) send(ctx context.Context, p payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("pagerduty: marshal payload: %w", err)
	}

	var lastErr error
	delay := retryDelay
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				delay *= 2
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("pagerduty: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("pagerduty: http error: %w", err)
			continue
		}
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("pagerduty: server error %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("pagerduty: client error %d (routing key or payload invalid)", resp.StatusCode)
		}
		return nil // 2xx
	}
	return fmt.Errorf("pagerduty: all %d attempts failed: %w", maxRetries, lastErr)
}
