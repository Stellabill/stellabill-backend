package outbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---

type mockSecretsProvider struct {
	secret string
	err    error
}

func (m *mockSecretsProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return m.secret, m.err
}

type mockSlackClient struct {
	statusCode int
	retryAfter string
	err        error
	// capture last request body
	lastBody []byte
}

func (m *mockSlackClient) PostSlack(_ context.Context, _ string, body []byte) (int, string, error) {
	m.lastBody = body
	return m.statusCode, m.retryAfter, m.err
}

// makeEvent builds a minimal Event for tests.
func makeEvent(eventType string) *Event {
	return &Event{
		ID:        uuid.New(),
		EventType: eventType,
	}
}

// noSleep replaces time.Sleep so tests don't block.
func noSleep(_ time.Duration) {}

// --- constructor ---

func TestNewSlackPublisher_NilClientUsesDefault(t *testing.T) {
	sp := NewSlackPublisher("key", &mockSecretsProvider{secret: "http://x"}, nil)
	require.NotNil(t, sp)
	assert.NotNil(t, sp.client)
}

// --- Publish: happy path ---

func TestSlackPublisher_Publish_Success(t *testing.T) {
	mc := &mockSlackClient{statusCode: http.StatusOK}
	sp := NewSlackPublisher("slack_webhook", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	err := sp.Publish(context.Background(), makeEvent("subscription.created"))
	require.NoError(t, err)
	assert.NotEmpty(t, mc.lastBody)
}

func TestSlackPublisher_Publish_AllBuiltinTemplates(t *testing.T) {
	for _, et := range []string{"subscription.created", "subscription.charged", "subscription.cancelled", "test.event"} {
		mc := &mockSlackClient{statusCode: 200}
		sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
		sp.sleepFn = noSleep
		require.NoError(t, sp.Publish(context.Background(), makeEvent(et)), "event type: %s", et)
	}
}

// --- Publish: secrets errors ---

func TestSlackPublisher_Publish_SecretError(t *testing.T) {
	sp := NewSlackPublisher("k", &mockSecretsProvider{err: errors.New("vault down")}, nil)
	sp.sleepFn = noSleep
	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault down")
	assert.False(t, IsPermanentPublishError(err), "secret error should be transient")
}

func TestSlackPublisher_Publish_EmptyWebhookURL(t *testing.T) {
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: ""}, nil)
	sp.sleepFn = noSleep
	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
	assert.Contains(t, err.Error(), "webhook URL is empty")
}

// --- Publish: unknown event type → dead-letter ---

func TestSlackPublisher_Publish_UnknownEventType(t *testing.T) {
	mc := &mockSlackClient{statusCode: 200}
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	err := sp.Publish(context.Background(), makeEvent("billing.unknown"))
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
	assert.Contains(t, err.Error(), "no template for event type")
}

// --- Publish: 429 rate-limiting ---

func TestSlackPublisher_Publish_RateLimited_SecondsSleep(t *testing.T) {
	mc := &mockSlackClient{statusCode: http.StatusTooManyRequests, retryAfter: "3"}
	var slept time.Duration
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = func(d time.Duration) { slept = d }

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.False(t, IsPermanentPublishError(err), "429 is transient")
	assert.Contains(t, err.Error(), "rate limited")
	assert.Equal(t, 3*time.Second, slept)
}

func TestSlackPublisher_Publish_RateLimited_NoRetryAfterHeader(t *testing.T) {
	mc := &mockSlackClient{statusCode: http.StatusTooManyRequests, retryAfter: ""}
	var slept time.Duration
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = func(d time.Duration) { slept = d }

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.Equal(t, time.Second, slept, "fallback 1s when header absent")
}

func TestSlackPublisher_Publish_RateLimited_HTTPDateRetryAfter(t *testing.T) {
	future := time.Now().UTC().Add(5 * time.Second).Format(http.TimeFormat)
	mc := &mockSlackClient{statusCode: http.StatusTooManyRequests, retryAfter: future}
	var slept time.Duration
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = func(d time.Duration) { slept = d }

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.Greater(t, slept, time.Duration(0))
}

func TestSlackPublisher_Publish_RateLimited_PastHTTPDate(t *testing.T) {
	past := time.Now().UTC().Add(-5 * time.Second).Format(http.TimeFormat)
	mc := &mockSlackClient{statusCode: http.StatusTooManyRequests, retryAfter: past}
	var slept time.Duration
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = func(d time.Duration) { slept = d }

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.Equal(t, time.Second, slept, "past date falls back to 1s")
}

func TestSlackPublisher_Publish_RateLimited_InvalidRetryAfter(t *testing.T) {
	mc := &mockSlackClient{statusCode: http.StatusTooManyRequests, retryAfter: "garbage"}
	var slept time.Duration
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = func(d time.Duration) { slept = d }

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.Equal(t, time.Second, slept, "invalid header falls back to 1s")
}

// --- Publish: 4xx non-429 → permanent dead-letter ---

func TestSlackPublisher_Publish_4xxNon429_PermanentError(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 410, 422} {
		mc := &mockSlackClient{statusCode: code}
		sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
		sp.sleepFn = noSleep

		err := sp.Publish(context.Background(), makeEvent("test.event"))
		require.Error(t, err, "expected error for status %d", code)
		assert.True(t, IsPermanentPublishError(err), "status %d should be permanent", code)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d", code))
	}
}

// --- Publish: 5xx → transient ---

func TestSlackPublisher_Publish_5xx_TransientError(t *testing.T) {
	mc := &mockSlackClient{statusCode: 500}
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.False(t, IsPermanentPublishError(err))
	assert.Contains(t, err.Error(), "server error 500")
}

// --- Publish: transport error → transient ---

func TestSlackPublisher_Publish_TransportError(t *testing.T) {
	mc := &mockSlackClient{err: errors.New("connection refused")}
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	err := sp.Publish(context.Background(), makeEvent("test.event"))
	require.Error(t, err)
	assert.False(t, IsPermanentPublishError(err))
	assert.Contains(t, err.Error(), "connection refused")
}

// --- RegisterTemplate ---

func TestSlackPublisher_RegisterTemplate_Custom(t *testing.T) {
	mc := &mockSlackClient{statusCode: 200}
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	sp.RegisterTemplate("custom.event", func(e *Event) (*slackPayload, error) {
		return &slackPayload{Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "plain_text", Text: "custom"}},
		}}, nil
	})

	err := sp.Publish(context.Background(), makeEvent("custom.event"))
	require.NoError(t, err)
}

func TestSlackPublisher_RegisterTemplate_Error(t *testing.T) {
	mc := &mockSlackClient{statusCode: 200}
	sp := NewSlackPublisher("k", &mockSecretsProvider{secret: "http://hook"}, mc)
	sp.sleepFn = noSleep

	sp.RegisterTemplate("bad.template", func(e *Event) (*slackPayload, error) {
		return nil, errors.New("template exploded")
	})

	err := sp.Publish(context.Background(), makeEvent("bad.template"))
	require.Error(t, err)
	assert.True(t, IsPermanentPublishError(err))
	assert.Contains(t, err.Error(), "template exploded")
}

// --- defaultSlackClient integration (real HTTP) ---

func TestDefaultSlackClient_PostSlack_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewDefaultSlackClient(2 * time.Second)
	code, ra, err := c.PostSlack(context.Background(), srv.URL, []byte(`{"blocks":[]}`))
	require.NoError(t, err)
	assert.Equal(t, 200, code)
	assert.Empty(t, ra)
}

func TestDefaultSlackClient_PostSlack_RetryAfterHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewDefaultSlackClient(2 * time.Second)
	code, ra, err := c.PostSlack(context.Background(), srv.URL, []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, 429, code)
	assert.Equal(t, "60", ra)
}

func TestDefaultSlackClient_PostSlack_InvalidURL(t *testing.T) {
	c := NewDefaultSlackClient(time.Second)
	_, _, err := c.PostSlack(context.Background(), "://bad-url", []byte(`{}`))
	require.Error(t, err)
}

func TestDefaultSlackClient_PostSlack_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := NewDefaultSlackClient(2 * time.Second)
	_, _, err := c.PostSlack(ctx, srv.URL, []byte(`{}`))
	require.Error(t, err)
}

func TestDefaultSlackClient_ZeroTimeout_UsesDefault(t *testing.T) {
	c := NewDefaultSlackClient(0)
	require.NotNil(t, c)
}

// --- parseRetryAfter (exported edge cases via Publish path are covered above) ---

func TestParseRetryAfter_ZeroSeconds(t *testing.T) {
	// strconv.Atoi succeeds but secs == 0 → fallback
	d := parseRetryAfter("0")
	assert.Equal(t, time.Second, d)
}

func TestParseRetryAfter_RFC850(t *testing.T) {
	future := time.Now().UTC().Add(10 * time.Second).Format(time.RFC850)
	d := parseRetryAfter(future)
	assert.Greater(t, d, time.Duration(0))
}

func TestParseRetryAfter_ANSIC(t *testing.T) {
	future := time.Now().UTC().Add(10 * time.Second).Format(time.ANSIC)
	d := parseRetryAfter(future)
	assert.Greater(t, d, time.Duration(0))
}
