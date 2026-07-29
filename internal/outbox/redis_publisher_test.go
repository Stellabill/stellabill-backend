package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRedisPublisher creates a miniredis server and a RedisPublisher backed by it.
func setupRedisPublisher(t *testing.T) (*RedisPublisher, *miniredis.Miniredis) {
	t.Helper()
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	p, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
	})
	require.NoError(t, err)

	return p, s
}

func makeTestEvent() *Event {
	data, _ := json.Marshal(EventData{
		Type:      "test.event",
		Data:      map[string]string{"foo": "bar"},
		Timestamp: time.Now(),
		ID:        uuid.New().String(),
	})
	return &Event{
		ID:            uuid.New(),
		TenantID:      "tenant-1",
		EventType:     "test.event",
		EventData:     data,
		AggregateID:   stringPtr("agg-1"),
		AggregateType: stringPtr("subscription"),
		OccurredAt:    time.Now(),
		Version:       1,
	}
}

func TestRedisPublisher_Publish_Success(t *testing.T) {
	p, s := setupRedisPublisher(t)
	event := makeTestEvent()

	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	// Verify the stream has the event
	streamLen, err := s.XLen(p.stream)
	require.NoError(t, err)
	assert.Equal(t, 1, streamLen)

	// Verify stream contents
	entries, err := s.XReadRange(p.stream, "-", "+", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, event.ID.String(), entry.Values["id"])
	assert.Equal(t, event.EventType, entry.Values["event_type"])
	assert.Equal(t, string(event.EventData), entry.Values["event_data"])
	assert.Equal(t, *event.AggregateID, entry.Values["aggregate_id"])
	assert.Equal(t, *event.AggregateType, entry.Values["aggregate_type"])
	assert.Equal(t, event.TenantID, entry.Values["tenant_id"])
	assert.Equal(t, "1", entry.Values["version"])
}

func TestRedisPublisher_Publish_MaxLenCap(t *testing.T) {
	p, s := setupRedisPublisher(t)
	maxLen := int64(10)

	// Use a publisher with a smaller maxlen
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	p, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
		MaxLen: maxLen,
	})
	require.NoError(t, err)

	// Publish more events than the cap
	for i := 0; i < 20; i++ {
		event := makeTestEvent()
		// Give each event a unique ID so miniredis treats them as distinct entries
		event.ID = uuid.New()
		require.NoError(t, p.Publish(context.Background(), event))
	}

	// Stream should be capped at ~maxLen entries
	streamLen, err := s.XLen(p.stream)
	require.NoError(t, err)
	assert.LessOrEqual(t, streamLen, int(maxLen))
}

func TestRedisPublisher_ConsumerGroupCreated(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	p, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
		Stream: "test:stream",
		Group:  "test-group",
	})
	require.NoError(t, err)
	require.NotNil(t, p)

	// Verify the group exists
	groups, err := s.XInfoGroups("test:stream")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "test-group", groups[0].Name)
}

func TestRedisPublisher_ConsumerGroupIdempotent(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	// Create the group once
	p1, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
		Group:  "dup-group",
	})
	require.NoError(t, err)
	require.NotNil(t, p1)

	// Create it again — should not error
	p2, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
		Group:  "dup-group",
	})
	require.NoError(t, err)
	require.NotNil(t, p2)
}

func TestRedisPublisher_Publish_DefaultConfig(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	p, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
	})
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, defaultRedisStream, p.stream)
	assert.Equal(t, defaultRedisGroup, p.group)
	assert.Equal(t, defaultRedisMaxLen, p.maxLen)
	assert.Equal(t, false, p.approx)
}

func TestRedisPublisher_Publish_WithApproxMaxLen(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })

	p, err := NewRedisPublisher(RedisPublisherConfig{
		Client:       client,
		MaxLen:       100,
		MaxLenApprox: true,
	})
	require.NoError(t, err)

	err = p.Publish(context.Background(), makeTestEvent())
	require.NoError(t, err)
}

func TestRedisPublisher_Publish_ConnectionRefused(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { client.Close() })

	// We create the publisher without ensureGroup by using a direct struct
	// to avoid the startup check failing
	p := &RedisPublisher{
		client: client,
		stream: "test:stream",
		group:  "test-group",
		maxLen: 1000,
	}

	err := p.Publish(context.Background(), makeTestEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis: xadd")
}

func TestRedisPublisher_HistogramMetricRecorded(t *testing.T) {
	p, s := setupRedisPublisher(t)

	before := testutil.ToFloat64(redisPublishDuration)

	err := p.Publish(context.Background(), makeTestEvent())
	require.NoError(t, err)

	// Force the stream to advance so miniredis picks up the entry
	_, err = s.XLen(p.stream)
	require.NoError(t, err)

	after := testutil.ToFloat64(redisPublishDuration)
	assert.Greater(t, after, before, "histogram should increment after a publish")
}

func TestRedisPublisher_Publish_MovedRedirect(t *testing.T) {
	// Set up two miniredis servers — the first will trigger a MOVED error
	// pointing to the second, which should handle the XADD.
	s1 := miniredis.RunT(t)
	s2 := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: s1.Addr()})
	t.Cleanup(func() { client.Close() })

	p := &RedisPublisher{
		client: client,
		stream: "test:stream",
		group:  "test-group",
		maxLen: 1000,
	}

	event := makeTestEvent()

	// Override the client with one that returns MOVED on the first call
	var callCount int32
	mockClient := &movedInjectorClient{
		inner:   client,
		onFirst: func() error {
			return &redis.MovedError{Slot: 1234, Addr: s2.Addr()}
		},
		callCount: &callCount,
	}
	p.client = mockClient

	err := p.Publish(context.Background(), event)
	require.NoError(t, err, "should handle MOVED redirect and succeed")

	// The event should be in stream on s2
	entries, err := s2.XReadRange("test:stream", "-", "+", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, event.ID.String(), entries[0].Values["id"])
}

// movedInjectorClient wraps a redis.Client and injects a MOVED error on the
// first XAdd call, then delegates to the real client for retry and subsequent
// calls.
type movedInjectorClient struct {
	inner     *redis.Client
	onFirst   func() error
	callCount *int32
}

func (m *movedInjectorClient) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	if atomic.AddInt32(m.callCount, 1) == 1 && m.onFirst != nil {
		return redis.NewStringResult("", m.onFirst())
	}
	return m.inner.XAdd(ctx, a)
}

func (m *movedInjectorClient) XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd {
	return m.inner.XGroupCreateMkStream(ctx, stream, group, start)
}

func TestRedisPublisher_Publish_AskRedirect(t *testing.T) {
	s1 := miniredis.RunT(t)
	s2 := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: s1.Addr()})
	t.Cleanup(func() { client.Close() })

	p := &RedisPublisher{
		client: client,
		stream: "test:stream",
		group:  "test-group",
		maxLen: 1000,
	}

	event := makeTestEvent()

	var callCount int32
	mockClient := &askInjectorClient{
		inner:   client,
		onFirst: func() error {
			return &redis.AskError{Slot: 1234, Addr: s2.Addr()}
		},
		callCount: &callCount,
	}
	p.client = mockClient

	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	entries, err := s2.XReadRange("test:stream", "-", "+", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, event.ID.String(), entries[0].Values["id"])
}

type askInjectorClient struct {
	inner     *redis.Client
	onFirst   func() error
	callCount *int32
}

func (m *askInjectorClient) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	if atomic.AddInt32(m.callCount, 1) == 1 && m.onFirst != nil {
		return redis.NewStringResult("", m.onFirst())
	}
	return m.inner.XAdd(ctx, a)
}

func (m *askInjectorClient) XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd {
	return m.inner.XGroupCreateMkStream(ctx, stream, group, start)
}

func TestRedisPublisher_Publish_MovedPermanentError(t *testing.T) {
	// When MOVED redirect also fails, the error should be returned
	s1 := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: s1.Addr()})
	t.Cleanup(func() { client.Close() })

	p := &RedisPublisher{
		client: client,
		stream: "test:stream",
		group:  "test-group",
		maxLen: 1000,
	}

	var callCount int32
	mockClient := &movedInjectorClient{
		inner: client,
		onFirst: func() error {
			return &redis.MovedError{Slot: 1234, Addr: "127.0.0.1:1"}
		},
		callCount: &callCount,
	}
	p.client = mockClient

	err := p.Publish(context.Background(), makeTestEvent())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis: xadd redirect to")
}

func TestRedisPublisher_EventToValues(t *testing.T) {
	p := &RedisPublisher{}

	event := makeTestEvent()
	values := p.eventToValues(event)

	assert.Equal(t, event.ID.String(), values["id"])
	assert.Equal(t, event.EventType, values["event_type"])
	assert.Equal(t, string(event.EventData), values["event_data"])
	assert.Equal(t, *event.AggregateID, values["aggregate_id"])
	assert.Equal(t, *event.AggregateType, values["aggregate_type"])
	assert.Equal(t, event.TenantID, values["tenant_id"])
	assert.Equal(t, "1", values["version"])
	assert.Contains(t, values["occurred_at"].(string), event.OccurredAt.Format(time.RFC3339Nano[:13]))
}

func TestRedisPublisher_EventToValues_NilPointers(t *testing.T) {
	p := &RedisPublisher{}
	event := &Event{
		ID:        uuid.New(),
		EventType: "test",
		EventData: json.RawMessage(`{}`),
		OccurredAt: time.Now(),
		Version:   1,
	}

	values := p.eventToValues(event)

	assert.Equal(t, event.ID.String(), values["id"])
	assert.Equal(t, event.EventType, values["event_type"])
	_, hasAggID := values["aggregate_id"]
	assert.False(t, hasAggID, "should not contain aggregate_id when nil")
	_, hasAggType := values["aggregate_type"]
	assert.False(t, hasAggType, "should not contain aggregate_type when nil")
	_, hasTenant := values["tenant_id"]
	assert.False(t, hasTenant, "should not contain tenant_id when empty")
}

func TestRedisPublisher_NewRedisPublisher_InvalidAddr(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1",
	})
	t.Cleanup(func() { client.Close() })

	_, err := NewRedisPublisher(RedisPublisherConfig{
		Client: client,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis: ensure consumer group")
}

func TestRedisPublisher_Publish_TransientError(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { client.Close() })

	p := &RedisPublisher{
		client: client,
		stream: "test:stream",
		group:  "test-group",
		maxLen: 1000,
	}

	err := p.Publish(context.Background(), makeTestEvent())
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || containsDialError(err), "expected a dial/connection error")
}

func containsDialError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "connection refused"))
}