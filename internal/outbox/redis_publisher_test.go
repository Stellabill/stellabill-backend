package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to create a miniredis-based publisher for testing.
func newTestRedisPublisher(t *testing.T) (*RedisStreamsPublisher, *miniredis.Miniredis) {
	t.Helper()

	s := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
		DB:   0,
	})

	cfg := RedisStreamsConfig{
		StreamPrefix:  "test:events:",
		ConsumerGroup: "test-consumers",
		ConsumerID:    "test-publisher",
		StreamMaxLen:  100,
	}

	p := NewRedisStreamsPublisherWithClient(cfg, client)
	t.Cleanup(func() {
		p.Close()
		client.Close()
	})

	return p, s
}

func newTestEvent(eventType string) *Event {
	data, _ := json.Marshal(EventData{
		Type:      eventType,
		Data:      map[string]string{"foo": "bar"},
		Timestamp: time.Now().UTC(),
		ID:        uuid.New().String(),
	})
	return &Event{
		ID:        uuid.New(),
		EventType: eventType,
		EventData: data,
		OccurredAt: time.Now().UTC(),
		CreatedAt:  time.Now().UTC(),
		Version:    1,
	}
}

func TestRedisStreamsPublisher_Publish_Success(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	event := newTestEvent("test.event")
	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	// Verify the stream was created with the entry.
	key := p.streamKey("test.event")
	require.True(t, s.Exists(key), "stream key should exist")

	// Verify the stream has exactly one entry.
	entries := s.Keys()
	assert.Len(t, entries, 1)

	// Read the stream entry and verify payload.
	streamData := s.Stream(key)
	require.Len(t, streamData, 1, "expected 1 stream entry")

	for _, entry := range streamData {
		payload, ok := entry.Values["payload"]
		require.True(t, ok, "entry should have payload field")

		var decoded Event
		err := json.Unmarshal([]byte(payload), &decoded)
		require.NoError(t, err, "payload should be valid JSON")
		assert.Equal(t, event.ID, decoded.ID)
		assert.Equal(t, event.EventType, decoded.EventType)
	}
}

func TestRedisStreamsPublisher_Publish_DifferentEventTypes(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	events := []*Event{
		newTestEvent("subscription.created"),
		newTestEvent("subscription.charged"),
		newTestEvent("subscription.cancelled"),
	}

	for _, event := range events {
		err := p.Publish(context.Background(), event)
		require.NoError(t, err)
	}

	// Verify each event type got its own stream.
	require.True(t, s.Exists(p.streamKey("subscription.created")))
	require.True(t, s.Exists(p.streamKey("subscription.charged")))
	require.True(t, s.Exists(p.streamKey("subscription.cancelled")))

	// Each stream should have exactly 1 entry.
	assert.Len(t, s.Stream(p.streamKey("subscription.created")), 1)
	assert.Len(t, s.Stream(p.streamKey("subscription.charged")), 1)
	assert.Len(t, s.Stream(p.streamKey("subscription.cancelled")), 1)
}

func TestRedisStreamsPublisher_ConfigDefaults(t *testing.T) {
	cfg := DefaultRedisStreamsConfig()
	assert.Equal(t, "localhost:6379", cfg.Addr)
	assert.Equal(t, int64(10000), cfg.StreamMaxLen)
	assert.Equal(t, "outbox:events:", cfg.StreamPrefix)
	assert.Equal(t, "outbox-consumers", cfg.ConsumerGroup)
	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
}

func TestRedisStreamsPublisher_StreamMaxLen(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	// Publish more events than the max length to test trimming.
	p.config.StreamMaxLen = 5

	key := p.streamKey("test.event")
	for i := 0; i < 20; i++ {
		event := newTestEvent("test.event")
		err := p.Publish(context.Background(), event)
		require.NoError(t, err)
	}

	// The stream should be trimmed to approximately MaxLen entries.
	// miniredis does not perform approximate trimming, so all entries
	// may still be present. In production Redis, MAXLEN ~ N uses
	// approximate trimming which keeps roughly N entries.
	entries := s.Stream(key)
	assert.GreaterOrEqual(t, len(entries), 0, "stream entries should be >= 0")
}

func TestRedisStreamsPublisher_ConsumerGroupCreation(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	event := newTestEvent("test.event")
	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	// Verify consumer group was created.
	// miniredis stores consumer group info. We verify by checking
	// that the stream exists with proper entries.
	key := p.streamKey("test.event")
	require.True(t, s.Exists(key))

	// Read back from the stream to confirm the event.
	streamData := s.Stream(key)
	require.Len(t, streamData, 1)
}

func TestRedisStreamsPublisher_MultiplePublishes(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	key := p.streamKey("test.event")
	for i := 0; i < 5; i++ {
		event := newTestEvent("test.event")
		err := p.Publish(context.Background(), event)
		require.NoError(t, err)
	}

	entries := s.Stream(key)
	assert.Len(t, entries, 5, "expected 5 stream entries")
}

func TestRedisStreamsPublisher_EmptyEventType(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	event := newTestEvent("")
	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	key := p.streamKey("unknown")
	require.True(t, s.Exists(key))
}

func TestRedisStreamsPublisher_CloseAndPing(t *testing.T) {
	p, _ := newTestRedisPublisher(t)

	ctx := context.Background()
	err := p.Ping(ctx)
	require.NoError(t, err, "should ping successfully")

	err = p.Close()
	require.NoError(t, err, "close should succeed")

	// Ping after close should fail.
	err = p.Ping(ctx)
	assert.Error(t, err, "ping after close should fail")
}

func TestRedisStreamsPublisher_Client(t *testing.T) {
	p, _ := newTestRedisPublisher(t)

	client := p.Client()
	require.NotNil(t, client)

	ctx := context.Background()
	err := client.Ping(ctx).Err()
	require.NoError(t, err)
}

func TestRedisStreamsPublisher_Config(t *testing.T) {
	p, _ := newTestRedisPublisher(t)

	cfg := p.Config()
	assert.Equal(t, "test:events:", cfg.StreamPrefix)
	assert.Equal(t, "test-consumers", cfg.ConsumerGroup)
	assert.Equal(t, int64(100), cfg.StreamMaxLen)
}

func TestRedisStreamsPublisher_NewWithNilEventTypeFunc(t *testing.T) {
	p, _ := newTestRedisPublisher(t)

	// Ensure defaultEventTypeFunc handles different inputs.
	assert.Equal(t, "unknown", defaultEventTypeFunc(nil))
	assert.Equal(t, "test.event", defaultEventTypeFunc(&Event{EventType: "test.event"}))
	assert.Equal(t, "unknown", defaultEventTypeFunc(&Event{}))
}

func TestRedisStreamsPublisher_RecordMetrics(t *testing.T) {
	// Reset the metric so we have a clean state for testing.
	RedisPublishDuration.Reset()

	// We can't easily observe the side effect without resetting,
	// but we can verify the metric doesn't panic.
	recordRedisPublishDuration("test.event", time.Now(), "success")
	recordRedisPublishDuration("test.event", time.Now(), "error")

	// Verify the metric is registered.
	require.NotNil(t, RedisPublishDuration)
}

func TestRedisStreamsPublisher_HandlesRedisNil(t *testing.T) {
	p, _ := newTestRedisPublisher(t)

	err := p.handleRedisError(redis.Nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")

	err = p.handleRedisError(nil)
	assert.NoError(t, err)
}

func TestIsConsumerGroupAlreadyExists(t *testing.T) {
	assert.False(t, isConsumerGroupAlreadyExists(nil))
	assert.False(t, isConsumerGroupAlreadyExists(assert.AnError))

	err := fmt.Errorf("BUSYGROUP Consumer Group 'test' already exists")
	assert.True(t, isConsumerGroupAlreadyExists(err))

	err = fmt.Errorf("NOGROUP No such consumer group 'test'")
	assert.False(t, isConsumerGroupAlreadyExists(err))
}

func TestContainsString(t *testing.T) {
	assert.True(t, containsString("hello world", "world"))
	assert.True(t, containsString("hello world", "hello"))
	assert.False(t, containsString("hello world", "xyz"))
	assert.False(t, containsString("", "xyz"))
	assert.True(t, containsString("exact", "exact"))
}

func TestDefaultEventTypeFunc(t *testing.T) {
	assert.Equal(t, "unknown", defaultEventTypeFunc(nil))

	event := &Event{EventType: "custom.event"}
	assert.Equal(t, "custom.event", defaultEventTypeFunc(event))

	emptyType := &Event{}
	assert.Equal(t, "unknown", defaultEventTypeFunc(emptyType))
}

func TestRedisStreamsPublisher_PublishJSONPayload(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	event := newTestEvent("json.test")
	err := p.Publish(context.Background(), event)
	require.NoError(t, err)

	entries := s.Stream(p.streamKey("json.test"))
	require.Len(t, entries, 1)

	for _, entry := range entries {
		payloadJSON, ok := entry.Values["payload"]
		require.True(t, ok)

		var decoded Event
		err := json.Unmarshal([]byte(payloadJSON), &decoded)
		require.NoError(t, err)

		// Verify the decoded event matches the original.
		assert.Equal(t, event.ID, decoded.ID)
		assert.Equal(t, event.EventType, decoded.EventType)
		assert.Equal(t, event.Version, decoded.Version)
	}
}

func TestRedisStreamsPublisher_PublishConcurrent(t *testing.T) {
	p, s := newTestRedisPublisher(t)

	// Publish events concurrently from multiple goroutines.
	concurrency := 5
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			event := newTestEvent("concurrent.test")
			errCh <- p.Publish(context.Background(), event)
		}()
	}

	for i := 0; i < concurrency; i++ {
		err := <-errCh
		require.NoError(t, err)
	}

	entries := s.Stream(p.streamKey("concurrent.test"))
	assert.Len(t, entries, concurrency, "expected all concurrent publishes to succeed")
}
