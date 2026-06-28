package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// slackBlock is a single Slack Block Kit block element.
type slackBlock struct {
	Type string    `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// slackPayload is the top-level Slack webhook payload.
type slackPayload struct {
	Blocks []slackBlock `json:"blocks"`
}

// SlackClient posts a raw JSON body to a URL and returns the HTTP status,
// the Retry-After header value (may be ""), and any transport error.
// A separate interface is used so the slack publisher can inspect headers
// without changing the shared HTTPClient interface.
type SlackClient interface {
	PostSlack(ctx context.Context, url string, body []byte) (statusCode int, retryAfter string, err error)
}

// defaultSlackClient wraps net/http to satisfy SlackClient.
type defaultSlackClient struct {
	client *http.Client
}

// NewDefaultSlackClient returns a SlackClient backed by a plain http.Client.
func NewDefaultSlackClient(timeout time.Duration) SlackClient {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &defaultSlackClient{client: &http.Client{Timeout: timeout}}
}

func (c *defaultSlackClient) PostSlack(ctx context.Context, url string, body []byte) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("slack: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("slack: http request: %w", err)
	}
	defer resp.Body.Close()
	// drain up to 4 KB so the connection can be reused.
	buf := make([]byte, 4096)
	_, _ = resp.Body.Read(buf)

	return resp.StatusCode, resp.Header.Get("Retry-After"), nil
}

// templateFunc builds a Slack payload for a given event.
type templateFunc func(event *Event) (*slackPayload, error)

// defaultTemplates maps event types to Block Kit builders.
var defaultTemplates = map[string]templateFunc{
	"subscription.created": func(e *Event) (*slackPayload, error) {
		return &slackPayload{Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf("*subscription.created*\nEvent `%s`", e.ID)}},
		}}, nil
	},
	"subscription.charged": func(e *Event) (*slackPayload, error) {
		return &slackPayload{Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf("*subscription.charged*\nEvent `%s`", e.ID)}},
		}}, nil
	},
	"subscription.cancelled": func(e *Event) (*slackPayload, error) {
		return &slackPayload{Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf("*subscription.cancelled*\nEvent `%s`", e.ID)}},
		}}, nil
	},
	"test.event": func(e *Event) (*slackPayload, error) {
		return &slackPayload{Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf("*test.event*\nEvent `%s`", e.ID)}},
		}}, nil
	},
}

// SlackPublisher publishes outbox events to a Slack channel via an
// Incoming Webhook URL obtained at runtime from the secrets provider.
//
// Retry-After (429): the publisher sleeps for the indicated duration and
// then returns a transient error so the dispatcher schedules a retry.
//
// 4xx non-429: treated as permanent (bad payload / misconfigured webhook);
// the event is routed straight to the dead-letter queue via PermanentPublishError.
type SlackPublisher struct {
	secretKey string
	secrets   SecretsProvider
	client    SlackClient
	templates map[string]templateFunc
	// sleepFn is swapped in tests to avoid real time.Sleep.
	sleepFn func(time.Duration)
}

// SecretsProvider is a narrow interface for retrieving secrets.
// It matches secrets.Provider so any implementation can be used directly.
type SecretsProvider interface {
	GetSecret(ctx context.Context, key string) (string, error)
}

// NewSlackPublisher creates a SlackPublisher that fetches the webhook URL
// from secrets[secretKey] on every Publish call.
func NewSlackPublisher(secretKey string, secrets SecretsProvider, client SlackClient) *SlackPublisher {
	if client == nil {
		client = NewDefaultSlackClient(0)
	}
	return &SlackPublisher{
		secretKey: secretKey,
		secrets:   secrets,
		client:    client,
		templates: defaultTemplates,
		sleepFn:   time.Sleep,
	}
}

// RegisterTemplate adds or replaces the Block Kit template for eventType.
func (p *SlackPublisher) RegisterTemplate(eventType string, fn templateFunc) {
	p.templates[eventType] = fn
}

// Publish implements outbox.Publisher.
func (p *SlackPublisher) Publish(ctx context.Context, event *Event) error {
	// Resolve webhook URL from secrets provider (never from config files).
	webhookURL, err := p.secrets.GetSecret(ctx, p.secretKey)
	if err != nil {
		return fmt.Errorf("slack: get webhook secret %q: %w", p.secretKey, err)
	}
	if webhookURL == "" {
		return &PermanentPublishError{Reason: "slack: webhook URL is empty"}
	}

	// Build payload from template registry.
	tmpl, ok := p.templates[event.EventType]
	if !ok {
		// Unknown event type → dead-letter immediately; no point retrying.
		return &PermanentPublishError{Reason: fmt.Sprintf("slack: no template for event type %q", event.EventType)}
	}

	payload, err := tmpl(event)
	if err != nil {
		return &PermanentPublishError{Reason: fmt.Sprintf("slack: template error for %q", event.EventType), Err: err}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &PermanentPublishError{Reason: "slack: marshal payload", Err: err}
	}

	statusCode, retryAfter, err := p.client.PostSlack(ctx, webhookURL, body)
	if err != nil {
		// Transport-level error → transient; dispatcher will retry.
		return fmt.Errorf("slack: post: %w", err)
	}

	switch {
	case statusCode == http.StatusTooManyRequests:
		delay := parseRetryAfter(retryAfter)
		p.sleepFn(delay)
		return fmt.Errorf("slack: rate limited (429), retry after %s", retryAfter)

	case statusCode >= 400 && statusCode < 500:
		// 4xx (non-429) → permanent; route to dead-letter.
		return &PermanentPublishError{Reason: fmt.Sprintf("slack: permanent client error %d", statusCode)}

	case statusCode >= 500:
		// 5xx → transient; dispatcher will retry.
		return fmt.Errorf("slack: server error %d", statusCode)
	}

	return nil
}

// parseRetryAfter parses the Retry-After header (seconds or HTTP-date).
// Falls back to 1 second when the header is absent or unparseable.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return time.Second
	}
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date format (RFC 1123 / RFC 850 / ANSI C).
	for _, layout := range []string{http.TimeFormat, time.RFC850, time.ANSIC} {
		if t, err := time.Parse(layout, header); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
			return time.Second
		}
	}
	return time.Second
}
