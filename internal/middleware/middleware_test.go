package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/baggage"
)

func TestBaggageMiddleware_Populated(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bag := baggage.FromContext(r.Context())
		
		tenant := bag.Member("tenant_id")
		if tenant.Value() != "t-999" {
			t.Errorf("expected tenant_id 't-999', got '%s'", tenant.Value())
		}

		customer := bag.Member("customer_id")
		if customer.Value() != "c-888" {
			t.Errorf("expected customer_id 'c-888', got '%s'", customer.Value())
		}
	})

	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), TenantIDKey, "t-999")
	ctx = context.WithValue(ctx, CustomerIDKey, "c-888")
	
	rr := httptest.NewRecorder()
	handler := BaggageMiddleware(nextHandler)
	handler.ServeHTTP(rr, req.WithContext(ctx))
}

func TestBaggageMiddleware_Empty(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bag := baggage.FromContext(r.Context())
		if bag.Len() != 0 {
			t.Errorf("expected empty baggage, got %d items", bag.Len())
		}
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler := BaggageMiddleware(nextHandler)
	handler.ServeHTTP(rr, req)
}