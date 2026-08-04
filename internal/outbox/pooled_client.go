package outbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"stellarbill-backend/internal/httpx"
)

// defaultHTTPPool is the shared per-host connection pool backing HTTP
// publishers built without an explicit httpx.Pool, giving outbound event
// delivery a connection budget, circuit breaker, and DNS-TTL-aware dialing
// per subscriber host instead of one ad hoc *http.Client per publisher.
var defaultHTTPPool = httpx.New(httpx.DefaultConfig())

// httpClient is the default HTTP client used by service.go when creating
// HTTP publishers. It routes through the shared connection pool.
var httpClient HTTPClient = NewPooledHTTPClient(defaultHTTPPool)

// PooledHTTPClient adapts an httpx.Pool to the outbox HTTPClient interface.
type PooledHTTPClient struct {
	pool *httpx.Pool
}

// NewPooledHTTPClient wraps pool as an outbox HTTPClient.
func NewPooledHTTPClient(pool *httpx.Pool) *PooledHTTPClient {
	if pool == nil {
		pool = defaultHTTPPool
	}
	return &PooledHTTPClient{pool: pool}
}

// Post implements HTTPClient by routing the request through the shared pool.
func (c *PooledHTTPClient) Post(ctx context.Context, url string, contentType string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.pool.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxBodySize = 1024 * 1024
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodySize))

	return resp.StatusCode, nil
}

// PooledSlackClient adapts an httpx.Pool to the outbox SlackClient interface.
type PooledSlackClient struct {
	pool *httpx.Pool
}

// NewPooledSlackClient wraps pool as an outbox SlackClient.
func NewPooledSlackClient(pool *httpx.Pool) *PooledSlackClient {
	if pool == nil {
		pool = defaultHTTPPool
	}
	return &PooledSlackClient{pool: pool}
}

// PostSlack implements SlackClient by routing the request through the shared pool.
func (c *PooledSlackClient) PostSlack(ctx context.Context, url string, body []byte) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("slack: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.pool.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("slack: http request: %w", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	_, _ = resp.Body.Read(buf)

	return resp.StatusCode, resp.Header.Get("Retry-After"), nil
}

