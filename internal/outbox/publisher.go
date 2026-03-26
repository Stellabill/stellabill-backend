package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// HTTPPublisher publishes events via HTTP (placeholder implementation)
type HTTPPublisher struct {
	endpoint string
	client   HTTPClient
}

// HTTPClient interface for HTTP operations (allows for mocking)
type HTTPClient interface {
	Post(url string, contentType string, body []byte) (int, error)
}

// DefaultHTTPClient is a simple HTTP client implementation
type DefaultHTTPClient struct{}

func (c *DefaultHTTPClient) Post(url string, contentType string, body []byte) (int, error) {
	// This is a placeholder implementation
	// In a real implementation, you would use http.Client
	log.Printf("Would send POST to %s with content-type %s and body: %s", url, contentType, string(body))
	return 200, nil
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
		""aggregate_type": event.AggregateType,
		"version":       event.Version,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	statusCode, err := p.client.Post(p.endpoint, "application/json", body)
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
func (p *MultiPublisher) Publish(event *Event) error {
	var lastError error
	
	for i, publisher := range p.publishers {
		if err := publisher.Publish(event); err != nil {
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
