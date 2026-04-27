package outbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"go.uber.org/zap"
	"stellarbill-backend/internal/httpclient"
	"stellarbill-backend/internal/security"
)

// HTTPPublisher publishes events via HTTP (placeholder implementation)
type HTTPPublisher struct {
	endpoint string
	client   HTTPClient
}

// HTTPClient interface for HTTP operations (allows for mocking)
type HTTPClient interface {
	Post(url string, contentType string, body []byte, idempotencyKey string) (int, error)
}

// DefaultHTTPClient is a simple HTTP client implementation (mock)
type DefaultHTTPClient struct{}

func (c *DefaultHTTPClient) Post(url string, contentType string, body []byte, idempotencyKey string) (int, error) {
	// This is a placeholder implementation
	// In a real implementation, you would use http.Client
	log.Printf("Would send POST to %s with content-type %s and body: %s", 
		security.MaskPII(url), 
		contentType, 
		security.MaskPII(string(body)))
	return 200, nil
}

// RealHTTPClient is an actual HTTP client using the resilient wrapper
type RealHTTPClient struct {
	client *httpclient.Client
}

// NewRealHTTPClient creates a resilient real HTTP client
func NewRealHTTPClient(endpoint string, logger *zap.Logger) *RealHTTPClient {
	u, _ := url.Parse(endpoint)
	host := "unknown"
	if u != nil && u.Host != "" {
		host = u.Host
	}
	return &RealHTTPClient{
		client: httpclient.NewClient(host, logger),
	}
}

func (c *RealHTTPClient) Post(endpoint string, contentType string, body []byte, idempotencyKey string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", contentType)
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
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
func (p *HTTPPublisher) Publish(event *Event) error {
	var eventData EventData
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	payload := map[string]interface{}{
		"id":            event.ID,
		"type":          event.EventType,
		"data":          eventData.Data,
		"occurred_at":   event.OccurredAt,
		"aggregate_id":  event.AggregateID,
		"aggregate_type": event.AggregateType,
		"version":       event.Version,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	statusCode, err := p.client.Post(p.endpoint, "application/json", body, event.ID.String())
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
func (p *ConsolePublisher) Publish(event *Event) error {
	var eventData EventData
	if err := json.Unmarshal(event.EventData, &eventData); err != nil {
		return fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	msg := fmt.Sprintf("Publishing event: ID=%s, Type=%s, Data=%+v, AggregateID=%s, AggregateType=%s",
		security.MaskPII(event.ID.String()),
		event.EventType,
		eventData.Data,
		safeString(event.AggregateID),
		safeString(event.AggregateType),
	)
	log.Printf("%s", security.MaskPII(msg))

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
func (p *MultiPublisher) Publish(event *Event) error {
	var lastError error
	
	for i, publisher := range p.publishers {
		if err := publisher.Publish(event); err != nil {
			lastError = fmt.Errorf("publisher %d failed: %w", i, err)
			log.Printf("Publisher %d failed: %v", i, security.RedactError(err))
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
