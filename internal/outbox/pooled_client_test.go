package outbox

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"stellarbill-backend/internal/httpx"
)

func TestPooledHTTPClient_Post_Success(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		traceparent := r.Header.Get("Traceparent")
		assert.NotEmpty(t, traceparent, "expected traceparent header to be propagated")

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, `{"test":"data"}`, string(body))

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	pool := httpx.New(httpx.DefaultConfig())
	client := NewPooledHTTPClient(pool)

	ctx := context.Background()
	reqCtx := context.WithValue(ctx, "test-key", "test-val")
	carrier := propagation.MapCarrier{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
	propCtx := otel.GetTextMapPropagator().Extract(reqCtx, carrier)

	statusCode, err := client.Post(propCtx, server.URL, "application/json", []byte(`{"test":"data"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, statusCode)
}

func TestPooledHTTPClient_Post_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pool := httpx.New(httpx.DefaultConfig())
	client := NewPooledHTTPClient(pool)

	statusCode, err := client.Post(context.Background(), server.URL, "application/json", []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, statusCode)
}

func TestPooledHTTPClient_Post_ConnectionRefused(t *testing.T) {
	cfg := httpx.DefaultConfig()
	cfg.DialTimeout = time.Second
	cfg.RequestTimeout = time.Second
	pool := httpx.New(cfg)
	client := NewPooledHTTPClient(pool)

	// Nothing listens on this reserved TCP port, so the dial must fail.
	_, err := client.Post(context.Background(), "http://127.0.0.1:1", "application/json", []byte(`{}`))
	assert.Error(t, err)
}

func TestNewService_DefaultsToSharedPoolWhenHTTPPoolUnset(t *testing.T) {
	// NewService requires a *sql.DB; here we only need to reach the
	// publisher-construction branch, so we pass nil and expect it to fail
	// later at repository access, not at publisher setup. This mirrors the
	// bug this wiring fixes: a zero-value client field (nil *http.Client)
	// used to panic the first time the publisher dialed out.
	_, err := NewService(nil, ServiceConfig{
		PublisherType: "http",
		HTTPEndpoint:  "http://example.invalid",
	})
	require.NoError(t, err)
}
