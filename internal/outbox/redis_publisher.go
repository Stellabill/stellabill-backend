package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStreamsConfig holds configuration for the Redis Streams publisher.
type RedisStreamsConfig struct {
	// Addr is the Redis server address (e.g. "localhost:6379").
	// For cluster mode, address discovery is handled by the RedisClusterClient.
	Addr string

	// Password is the Redis ACL password. Leave empty when authentication is
	// not required.
	Password string

	// DB is the Redis database number (ignored in cluster mode).
	DB int

	// StreamMaxLen is the approximate maximum length of each event stream.
	// The publisher uses MAXLEN ~ N to cap stream memory without blocking.
	// When zero, no MAXLEN trimming is applied.
	StreamMaxLen int64

	// StreamPrefix is the prefix for stream keys (e.g. "outbox:events:").
	// Each topic/event-type gets its own stream key: <prefix><event_type>.
	StreamPrefix string

	// ConsumerGroup is the consumer group name for the stream. All publisher
	// instances share the same group for at-least-once delivery semantics.
	ConsumerGroup string

	// ConsumerID is the unique consumer identifier within the group.
	ConsumerID string

	// ClusterMode enables Redis cluster-specific features: MOVED/ASK reply
	// handling and slot-based key routing.
	ClusterMode bool

	// DialTimeout is the timeout for connecting to Redis.
	DialTimeout time.Duration

	// ReadTimeout is the timeout for reading from Redis.
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for writing to Redis.
	WriteTimeout time.Duration

	// PoolSize is the maximum number of socket connections to Redis.
	PoolSize int

	// MinIdleConns is the minimum number of idle connections to keep warm.
	MinIdleConns int
}

// DefaultRedisStreamsConfig returns sensible defaults for the Redis publisher.
func DefaultRedisStreamsConfig() RedisStreamsConfig {
	return RedisStreamsConfig{
		Addr:           "localhost:6379",
		DB:             0,
		StreamMaxLen:   10000,
		StreamPrefix:   "outbox:events:",
		ConsumerGroup:  "outbox-consumers",
		ConsumerID:     "publisher-1",
		ClusterMode:    false,
		DialTimeout:    5 * time.Second,
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   3 * time.Second,
		PoolSize:       10,
		MinIdleConns:   3,
	}
}

// RedisStreamsPublisher publishes outbox events to Redis Streams with consumer
// group support for at-least-once delivery semantics.
//
// Events are stored in per-topic streams keyed as <prefix><event_type>.
// Stream length is capped using MAXLEN ~ N to control memory usage.
// A redis_publish_duration_seconds histogram is emitted for observability.
//
// In cluster mode, MOVED and ASK redirections are handled transparently by the
// go-redis cluster client.
type RedisStreamsPublisher struct {
	client        redis.UniversalClient
	config        RedisStreamsConfig
	eventTypeFunc func(*Event) string
}

// redisUniversalClient is a type alias for the universal client interface
// that satisfies both single-node and cluster Redis clients.
type redisUniversalClient = redis.UniversalClient

// NewRedisStreamsPublisher creates a new Redis Streams publisher.
// When cfg.Addr is empty, DefaultRedisStreamsConfig is used.
// When cfg.ClusterMode is true, a RedisClusterClient is created; otherwise
// a single-node Redis client is used.
func NewRedisStreamsPublisher(cfg RedisStreamsConfig) (*RedisStreamsPublisher, error) {
	if cfg.Addr == "" {
		cfg = DefaultRedisStreamsConfig()
	}
	if cfg.StreamPrefix == "" {
		cfg.StreamPrefix = "outbox:events:"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "outbox-consumers"
	}
	if cfg.ConsumerID == "" {
		cfg.ConsumerID = fmt.Sprintf("publisher-%d", time.Now().UnixNano())
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 3 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 3 * time.Second
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 10
	}
	if cfg.MinIdleConns <= 0 {
		cfg.MinIdleConns = 3
	}

	var client redis.UniversalClient
	if cfg.ClusterMode {
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:         []string{cfg.Addr},
			Password:      cfg.Password,
			DialTimeout:   cfg.DialTimeout,
			ReadTimeout:   cfg.ReadTimeout,
			WriteTimeout:  cfg.WriteTimeout,
			PoolSize:      cfg.PoolSize,
			MinIdleConns:  cfg.MinIdleConns,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:          cfg.Addr,
			Password:      cfg.Password,
			DB:            cfg.DB,
			DialTimeout:   cfg.DialTimeout,
			ReadTimeout:   cfg.ReadTimeout,
			WriteTimeout:  cfg.WriteTimeout,
			PoolSize:      cfg.PoolSize,
			MinIdleConns:  cfg.MinIdleConns,
		})
	}

	return &RedisStreamsPublisher{
		client:        client,
		config:        cfg,
		eventTypeFunc: defaultEventTypeFunc,
	}, nil
}

// NewRedisStreamsPublisherWithClient creates a publisher using an existing
// Redis client. This is useful for testing with miniredis or when sharing
// a client across components.
func NewRedisStreamsPublisherWithClient(cfg RedisStreamsConfig, client redis.UniversalClient) *RedisStreamsPublisher {
	if cfg.StreamPrefix == "" {
		cfg.StreamPrefix = "outbox:events:"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "outbox-consumers"
	}
	if cfg.StreamMaxLen <= 0 {
		cfg.StreamMaxLen = 10000
	}
	return &RedisStreamsPublisher{
		client:        client,
		config:        cfg,
		eventTypeFunc: defaultEventTypeFunc,
	}
}

// defaultEventTypeFunc extracts the event type from an Event.
func defaultEventTypeFunc(event *Event) string {
	if event == nil {
		return "unknown"
	}
	if event.EventType != "" {
		return event.EventType
	}
	return "unknown"
}

// streamKey returns the Redis stream key for the given event type.
func (p *RedisStreamsPublisher) streamKey(eventType string) string {
	return p.config.StreamPrefix + eventType
}

// Publish writes an event to the Redis stream with at-least-once semantics.
// It uses XADD with MAXLEN ~ N trimming and emits a duration histogram.
func (p *RedisStreamsPublisher) Publish(ctx context.Context, event *Event) error {
	start := time.Now()

	eventType := p.eventTypeFunc(event)
	key := p.streamKey(eventType)

	// Serialise the event as a JSON string for the stream field.
	eventJSON, err := json.Marshal(event)
	if err != nil {
		recordRedisPublishDuration(eventType, start, "error")
		return fmt.Errorf("redis: marshal event: %w", err)
	}

	// Build the stream entry arguments.
	args := &redis.XAddArgs{
		Stream: key,
		Values: map[string]interface{}{
			"event_type": eventType,
			"payload":    string(eventJSON),
			"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		},
	}

	// Apply MAXLEN trimming when configured.
	if p.config.StreamMaxLen > 0 {
		args.MaxLen = p.config.StreamMaxLen
		args.Approx = true // MAXLEN ~ N (approximate trimming)
	}

	// Execute XADD.
	id, err := p.client.XAdd(ctx, args).Result()
	if err != nil {
		err = p.handleRedisError(err)
		recordRedisPublishDuration(eventType, start, "error")
		return fmt.Errorf("redis: xadd %s: %w", key, err)
	}

	_ = id // The stream entry ID is available for tracing if needed.

	// Ensure consumer group exists (idempotent).
	if p.config.ConsumerGroup != "" {
		if err := p.ensureConsumerGroup(ctx, key); err != nil {
			// Non-fatal: the event was already published.
			recordRedisPublishDuration(eventType, start, "group_error")
			return fmt.Errorf("redis: consumer group %s: %w", p.config.ConsumerGroup, err)
		}
	}

	recordRedisPublishDuration(eventType, start, "success")
	return nil
}

// ensureConsumerGroup creates the consumer group for the stream if it does not
// already exist. This is idempotent — XGROUP CREATE MKSTREAM creates both the
// stream and the group atomically.
func (p *RedisStreamsPublisher) ensureConsumerGroup(ctx context.Context, key string) error {
	err := p.client.XGroupCreate(ctx, key, p.config.ConsumerGroup, "0").Err()
	if err != nil && !isConsumerGroupAlreadyExists(err) {
		return p.handleRedisError(err)
	}
	return nil
}

// isConsumerGroupAlreadyExists checks whether the error indicates the consumer
// group already exists (BUSYGROUP).
func isConsumerGroupAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return containsString(err.Error(), "BUSYGROUP")
}

// handleRedisError translates Redis errors into standard Go errors.
// In cluster mode, MOVED and ASK errors from go-redis are handled
// transparently by the cluster client, so no explicit handling is needed here.
// This method wraps other errors for consistent reporting.
func (p *RedisStreamsPublisher) handleRedisError(err error) error {
	if err == nil {
		return nil
	}
	if err == redis.Nil {
		return fmt.Errorf("redis: key not found")
	}
	return err
}

// Close closes the underlying Redis connections.
func (p *RedisStreamsPublisher) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

// Ping verifies the Redis connection is alive.
func (p *RedisStreamsPublisher) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

// Client returns the underlying Redis client. This is exposed for testing
// and advanced use cases (e.g. manual stream consumption).
func (p *RedisStreamsPublisher) Client() redis.UniversalClient {
	return p.client
}

// Config returns a copy of the publisher's configuration.
func (p *RedisStreamsPublisher) Config() RedisStreamsConfig {
	return p.config
}

// -- Observability --

// redisPublishDurationBuckets defines the histogram buckets for publish latency
// in seconds. Covers typical Redis round-trips from sub-ms up to 5s.
var redisPublishDurationBuckets = []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// recordRedisPublishDuration records the publish duration and status.
// This function is a no-op when the publisher was created without a metrics
// registry — the outbox package's init registers the histogram.
func recordRedisPublishDuration(eventType string, start time.Time, status string) {
	if RedisPublishDuration == nil {
		return
	}
	duration := time.Since(start).Seconds()
	RedisPublishDuration.WithLabelValues(eventType, status).Observe(duration)
}

// containsString reports whether substr is in s.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && containsStringFunc(s, substr)
}

// containsStringFunc is a simple substring check that avoids importing strings.
func containsStringFunc(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
