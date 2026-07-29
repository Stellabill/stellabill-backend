package outbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// RedisPublisherConfig configures the Redis Streams publisher.
type RedisPublisherConfig struct {
	Client       *redis.Client
	Stream       string
	Group        string
	Consumer     string
	MaxLen       int64
	MaxLenApprox bool
}

const (
	defaultRedisStream = "outbox:events"
	defaultRedisGroup  = "outbox-consumers"
	defaultRedisMaxLen = 1000
)

var redisPublishDuration prometheus.Histogram

func init() {
	redisPublishDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "redis_publish_duration_seconds",
		Help:    "Duration of Redis Streams XADD operations",
		Buckets: prometheus.DefBuckets,
	})
	_ = prometheus.Register(redisPublishDuration)
}

// redisStreamClient is the subset of redis.Client needed by RedisPublisher.
type redisStreamClient interface {
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd
}

// RedisPublisher publishes events to a Redis Stream with consumer group support.
type RedisPublisher struct {
	client   redisStreamClient
	stream   string
	group    string
	consumer string
	maxLen   int64
	approx   bool
}

type permanentError struct {
	reason string
}

func (e *permanentError) Error() string { return e.reason }

// NewRedisPublisher creates a new RedisPublisher and ensures the consumer group exists.
func NewRedisPublisher(config RedisPublisherConfig) (*RedisPublisher, error) {
	stream := config.Stream
	if stream == "" {
		stream = defaultRedisStream
	}
	group := config.Group
	if group == "" {
		group = defaultRedisGroup
	}
	consumer := config.Consumer
	if consumer == "" {
		host, _ := os.Hostname()
		consumer = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	maxLen := config.MaxLen
	if maxLen <= 0 {
		maxLen = defaultRedisMaxLen
	}

	p := &RedisPublisher{
		client:   config.Client,
		stream:   stream,
		group:    group,
		consumer: consumer,
		maxLen:   maxLen,
		approx:   config.MaxLenApprox,
	}

	if err := p.ensureGroup(context.Background()); err != nil {
		return nil, fmt.Errorf("redis: ensure consumer group: %w", err)
	}

	return p, nil
}

func (p *RedisPublisher) ensureGroup(ctx context.Context) error {
	err := p.client.XGroupCreateMkStream(ctx, p.stream, p.group, "0").Err()
	if err != nil && !isBusyGroupError(err) {
		return err
	}
	return nil
}

func isBusyGroupError(err error) bool {
	return err != nil && len(err.Error()) >= 9 && err.Error()[:9] == "BUSYGROUP"
}

// Publish publishes an event to the Redis Stream.
func (p *RedisPublisher) Publish(ctx context.Context, event *Event) error {
	values := p.eventToValues(event)

	timer := prometheus.NewTimer(redisPublishDuration)

	err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		MaxLen: p.maxLen,
		Approx: p.approx,
		Values: values,
	}).Err()

	timer.ObserveDuration()

	if err != nil {
		var movedErr redis.MovedError
		if errors.As(err, &movedErr) {
			return p.redirectXAdd(ctx, movedErr.Addr, values)
		}
		var askErr redis.AskError
		if errors.As(err, &askErr) {
			return p.redirectXAdd(ctx, askErr.Addr, values)
		}
		return fmt.Errorf("redis: xadd: %w", err)
	}

	return nil
}

func (p *RedisPublisher) redirectXAdd(ctx context.Context, addr string, values map[string]interface{}) error {
	redirectClient := redis.NewClient(&redis.Options{Addr: addr})
	defer redirectClient.Close()

	timer := prometheus.NewTimer(redisPublishDuration)

	err := redirectClient.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		MaxLen: p.maxLen,
		Approx: p.approx,
		Values: values,
	}).Err()

	timer.ObserveDuration()

	if err != nil {
		return fmt.Errorf("redis: xadd redirect to %s: %w", addr, err)
	}
	return nil
}

func (p *RedisPublisher) eventToValues(event *Event) map[string]interface{} {
	values := map[string]interface{}{
		"id":          event.ID.String(),
		"event_type":  event.EventType,
		"event_data":  string(event.EventData),
		"occurred_at": event.OccurredAt.Format(time.RFC3339Nano),
		"version":     event.Version,
	}
	if event.AggregateID != nil {
		values["aggregate_id"] = *event.AggregateID
	}
	if event.AggregateType != nil {
		values["aggregate_type"] = *event.AggregateType
	}
	if event.TenantID != "" {
		values["tenant_id"] = event.TenantID
	}
	return values
}