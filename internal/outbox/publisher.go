package outbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type HTTPPublisher struct {
	endpoint string
	client   HTTPClient
}

// HTTPClient interface for HTTP operations (allows for mocking)
type HTTPClient interface {
	Post(ctx context.Context, url string, contentType string, body []byte) (int, error)
}

type DefaultHTTPClient struct {
	client *http.Client
}

func NewDefaultHTTPClient(timeout time.Duration, caFile string) (*DefaultHTTPClient, error) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file %s: %w", caFile, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", caFile)
		}
		transport.TLSClientConfig = &tls.Config{
			RootCAs:    caCertPool,
			MinVersion: tls.VersionTLS12,
		}
	}

	return &DefaultHTTPClient{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

func (c *DefaultHTTPClient) Post(ctx context.Context, url string, contentType string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxBodySize = 1024 * 1024
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodySize))

	return resp.StatusCode, nil
}

// NewHTTPPublisher creates a new HTTP publisher
func NewHTTPPublisher(endpoint string, client HTTPClient) Publisher {
	return &HTTPPublisher{
		endpoint: endpoint,
		client:   client,
	}
}

// Publish publishes an event via HTTP
func (p *HTTPPublisher) Publish(ctx context.Context, event *Event) error {
	var eventData EventData
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	payload := map[string]interface{}{
		"id":             event.ID,
		"type":           event.EventType,
		"data":           eventData.Data,
		"occurred_at":    event.OccurredAt,
		"aggregate_id":   event.AggregateID,
		"aggregate_type": event.AggregateType,
		"version":        event.Version,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	statusCode, err := p.client.Post(ctx, p.endpoint, "application/json", body)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	if statusCode >= 400 {
		return fmt.Errorf("HTTP request failed with status code: %d", statusCode)
	}

	return nil
}

// ConsolePublisher publishes events to console (for testing/development)
type ConsolePublisher struct{}

// NewConsolePublisher creates a new console publisher
func NewConsolePublisher() Publisher {
	return &ConsolePublisher{}
}

// Publish publishes an event to console
func (p *ConsolePublisher) Publish(ctx context.Context, event *Event) error {
	var eventData EventData
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	log.Printf("Publishing event: ID=%s, Type=%s, Data=%+v, AggregateID=%s, AggregateType=%s",
		event.ID,
		event.EventType,
		eventData.Data,
		safeString(event.AggregateID),
		safeString(event.AggregateType),
	)

	return nil
}

// MultiPublisher publishes to multiple publishers
type MultiPublisher struct {
	publishers []Publisher
}

// NewMultiPublisher creates a new multi-publisher
func NewMultiPublisher(publishers ...Publisher) Publisher {
	return &MultiPublisher{publishers: publishers}
}

// Publish publishes to all publishers
func (p *MultiPublisher) Publish(ctx context.Context, event *Event) error {
	var lastError error
	
	for i, publisher := range p.publishers {
		if err := publisher.Publish(ctx, event); err != nil {
			lastError = fmt.Errorf("publisher %d failed: %w", i, err)
			log.Printf("Publisher %d failed: %v", i, err)
		}
	}
	
	return lastError
}

// safeString safely dereferences a string pointer
func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}