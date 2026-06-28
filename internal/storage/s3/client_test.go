package s3_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	s3client "stellarbill-backend/internal/storage/s3"
)

// newTestClient builds a client pointed at a test server.
func newTestClient(endpoint string, maxRetries int) s3client.S3Uploader {
	return s3client.New(s3client.Config{
		Region:          "us-east-1",
		Bucket:          "test-bucket",
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "testsecret",
		Endpoint:        endpoint,
		MaxRetries:      maxRetries,
	})
}

// --- PutObject tests ---

func TestPutObject_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "test-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, 0)
	err := c.PutObject(context.Background(), "test-key", []byte("hello"), "text/plain")
	require.NoError(t, err)
}

func TestPutObject_Retry_On_5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, 3)
	err := c.PutObject(context.Background(), "key", []byte("data"), "")
	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "should have retried until success on 3rd call")
}

func TestPutObject_ExhaustsRetries_Returns_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // 500 always
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, 2)
	err := c.PutObject(context.Background(), "key", []byte("data"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after")
}

func TestPutObject_4xx_No_Retry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden) // 403 — should not retry
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, 3)
	err := c.PutObject(context.Background(), "key", []byte("data"), "")
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "4xx must not trigger retry")
	assert.Contains(t, err.Error(), "client error")
}

func TestPutObject_ContextCancellation(t *testing.T) {
	// Server that always returns 503 so client would retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to force early exit during backoff.
	cancel()

	c := newTestClient(srv.URL, 5)
	err := c.PutObject(ctx, "key", []byte("data"), "")
	require.Error(t, err)
}

// --- PresignURL tests ---

func TestPresignURL_HappyPath(t *testing.T) {
	c := newTestClient("https://example.com", 0)
	result, err := c.PresignURL(context.Background(), "exports/tenant-1/2025-01-01.csv.gz", 15*time.Minute)
	require.NoError(t, err)

	assert.NotEmpty(t, result.URL)
	assert.Contains(t, result.URL, "X-Amz-Signature")
	assert.Contains(t, result.URL, "X-Amz-Expires=900")
	assert.Contains(t, result.URL, "X-Amz-Credential")
	assert.True(t, result.ExpiresAt.After(time.Now()), "ExpiresAt should be in the future")
}

func TestPresignURL_TTL_Reflected_In_URL(t *testing.T) {
	c := newTestClient("https://example.com", 0)

	ttl := 5 * time.Minute
	result, err := c.PresignURL(context.Background(), "some-key", ttl)
	require.NoError(t, err)
	assert.Contains(t, result.URL, "X-Amz-Expires=300")
	assert.WithinDuration(t, time.Now().Add(ttl), result.ExpiresAt, 5*time.Second)
}

func TestPresignURL_DefaultEndpoint_Format(t *testing.T) {
	c := s3client.New(s3client.Config{
		Region:          "eu-west-1",
		Bucket:          "my-bucket",
		AccessKeyID:     "AK",
		SecretAccessKey: "SK",
		MaxRetries:      0,
	})
	result, err := c.PresignURL(context.Background(), "path/to/key", 15*time.Minute)
	require.NoError(t, err)
	assert.Contains(t, result.URL, "my-bucket.s3.eu-west-1.amazonaws.com")
}
