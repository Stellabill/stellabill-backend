package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/sdk/trace"
)

// mockSpan implements trace.ReadWriteSpan for testing purposes.
type mockSpan struct {
	trace.ReadWriteSpan
	attributes []attribute.KeyValue
}

func (m *mockSpan) SetAttributes(kv ...attribute.KeyValue) {
	m.attributes = append(m.attributes, kv...)
}

func TestBaggageSpanProcessor_OnStart(t *testing.T) {
	bsp := BaggageSpanProcessor{}

	m1, _ := baggage.NewMember("tenant_id", "t-123")
	m2, _ := baggage.NewMember("customer_id", "c-456")
	m3, _ := baggage.NewMember("pii_email", "user@example.com") // Target for rejection

	bag, _ := baggage.New(m1, m2, m3)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

	span := &mockSpan{}
	bsp.OnStart(ctx, span)

	if len(span.attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(span.attributes))
	}

	var foundTenant, foundCustomer bool
	for _, attr := range span.attributes {
		if attr.Key == "tenant_id" && attr.Value.AsString() == "t-123" {
			foundTenant = true
		}
		if attr.Key == "customer_id" && attr.Value.AsString() == "c-456" {
			foundCustomer = true
		}
		if attr.Key == "pii_email" {
			t.Fatalf("security failure: PII leaked into span attributes")
		}
	}

	if !foundTenant || !foundCustomer {
		t.Fatalf("missing required baggage attributes in span")
	}
}

func TestBaggageSpanProcessor_NoOps(t *testing.T) {
	bsp := BaggageSpanProcessor{}
	ctx := context.Background()
	if err := bsp.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown should return nil, got %v", err)
	}
	if err := bsp.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush should return nil, got %v", err)
	}
}