package outbox

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func writeCert(cert []byte, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert})
}

func TestDefaultHTTPClient_Post_Success(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		
		traceparent := r.Header.Get("Traceparent")
		assert.NotEmpty(t, traceparent, "expected traceparent header to be propagated")

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, `{"test":"data"}`, string(body))

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client, err := NewDefaultHTTPClient(5*time.Second, "")
	require.NoError(t, err)

	ctx := context.Background()
	reqCtx := context.WithValue(ctx, "test-key", "test-val")
	carrier := propagation.MapCarrier{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}
	propCtx := otel.GetTextMapPropagator().Extract(reqCtx, carrier)

	statusCode, err := client.Post(propCtx, server.URL, "application/json", []byte(`{"test":"data"}`))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, statusCode)
}

func TestDefaultHTTPClient_Post_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewDefaultHTTPClient(50*time.Millisecond, "")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client.Post(ctx, server.URL, "application/json", []byte(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestDefaultHTTPClient_TLSPinning(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	certFile, err := os.CreateTemp("", "ca-cert-*.pem")
	require.NoError(t, err)
	defer os.Remove(certFile.Name())

	err = writeCert(server.TLS.Certificates[0].Certificate[0], certFile.Name())
	require.NoError(t, err)

	client, err := NewDefaultHTTPClient(2*time.Second, certFile.Name())
	require.NoError(t, err)

	statusCode, err := client.Post(context.Background(), server.URL, "application/json", []byte{})
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	// Verify an unpinned client will reject the self-signed cert
	badClient, _ := NewDefaultHTTPClient(2*time.Second, "")
	_, err = badClient.Post(context.Background(), server.URL, "application/json", []byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "certificate signed by unknown authority")
}

func TestDefaultHTTPClient_BadCAFile(t *testing.T) {
	_, err := NewDefaultHTTPClient(2*time.Second, "non_existent_file.pem")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read CA file")

	badCertFile, err := os.CreateTemp("", "bad-cert-*.pem")
	require.NoError(t, err)
	defer os.Remove(badCertFile.Name())
	badCertFile.Write([]byte("not a real cert"))
	badCertFile.Close()

	_, err = NewDefaultHTTPClient(2*time.Second, badCertFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA certificate")
}

func TestDefaultHTTPClient_Post_MaxBodySize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		largeData := make([]byte, 2*1024*1024) 
		w.WriteHeader(http.StatusOK)
		w.Write(largeData)
	}))
	defer server.Close()

	client, err := NewDefaultHTTPClient(5*time.Second, "")
	require.NoError(t, err)

	statusCode, err := client.Post(context.Background(), server.URL, "application/json", []byte(`{}`))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)
}

func TestDefaultHTTPClient_Post_InvalidRequest(t *testing.T) {
	client, err := NewDefaultHTTPClient(5*time.Second, "")
	require.NoError(t, err)

	var nilCtx context.Context
	_, err = client.Post(nilCtx, "http://example.com", "application/json", []byte(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
}

type mockHTTPClient struct {
	statusCode int
	called     bool
	reqBody    []byte
}

func (m *mockHTTPClient) Post(ctx context.Context, url string, contentType string, body []byte) (int, error) {
	m.called = true
	m.reqBody = body
	return m.statusCode, nil
}

func TestHTTPPublisher_Publish_Success(t *testing.T) {
	mockClient := &mockHTTPClient{statusCode: 200}
	publisher := NewHTTPPublisher("http://example.com", mockClient)

	eventData := EventData{
		Type: "test",
		Data: map[string]string{"foo": "bar"},
	}
	dataBytes, _ := json.Marshal(eventData)

	event := &Event{
		ID:        uuid.New(),
		EventType: "test",
		EventData: dataBytes,
	}

	err := publisher.Publish(context.Background(), event)
	assert.NoError(t, err)
	assert.True(t, mockClient.called)
}

func TestHTTPPublisher_Publish_ErrorStatus(t *testing.T) {
	mockClient := &mockHTTPClient{statusCode: 500}
	publisher := NewHTTPPublisher("http://example.com", mockClient)

	eventData := EventData{Type: "test"}
	dataBytes, _ := json.Marshal(eventData)

	event := &Event{
		ID:        uuid.New(),
		EventType: "test",
		EventData: dataBytes,
	}

	err := publisher.Publish(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed with status code: 500")
}

func TestHTTPPublisher_Publish_BadEventData(t *testing.T) {
	mockClient := &mockHTTPClient{statusCode: 200}
	publisher := NewHTTPPublisher("http://example.com", mockClient)

	event := &Event{
		ID:        uuid.New(),
		EventType: "test",
		EventData: []byte(`{bad json`),
	}

	err := publisher.Publish(context.Background(), event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal event data")
}