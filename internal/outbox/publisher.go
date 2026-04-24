package outbox

import (
	"encoding/json"
	"fmt"

	"stellarbill-backend/internal/structuredlog"
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
	defaultLogger.Info("publishing outbox event over HTTP", structuredlog.Fields{
		structuredlog.FieldRequestID: "",
		structuredlog.FieldActor:     "system",
		structuredlog.FieldTenant:    "system",
		structuredlog.FieldRoute:     "outbox.publisher.http",
		structuredlog.FieldStatus:    "attempt",
		structuredlog.FieldDuration:  0,
		"endpoint":                   url,
		"content_type":               contentType,
		"payload_bytes":              len(body),
	})
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
		"aggregate_type": event.AggregateType,
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

	defaultLogger.Info("publishing outbox event to console", structuredlog.Fields{
		structuredlog.FieldRequestID: "",
		structuredlog.FieldActor:     "system",
		structuredlog.FieldTenant:    "system",
		structuredlog.FieldRoute:     "outbox.publisher.console",
		structuredlog.FieldStatus:    "attempt",
		structuredlog.FieldDuration:  0,
		"event_id":                   event.ID.String(),
		"event_type":                 event.EventType,
		"aggregate_id":               safeString(event.AggregateID),
		"aggregate_type":             safeString(event.AggregateType),
	})

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
			defaultLogger.Warn("one outbox publisher failed", structuredlog.Fields{
				structuredlog.FieldRequestID: "",
				structuredlog.FieldActor:     "system",
				structuredlog.FieldTenant:    "system",
				structuredlog.FieldRoute:     "outbox.publisher.multi",
				structuredlog.FieldStatus:    "publisher_failed",
				structuredlog.FieldDuration:  0,
				"publisher_index":            i,
				"event_id":                   event.ID.String(),
				"event_type":                 event.EventType,
				"error":                      err,
			})
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
