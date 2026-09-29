package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKafkaWriter struct {
	topic string
	ack   kafka.RequiredAcks
	msg   []kafka.Message
	err   error
}

func (f *fakeKafkaWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	f.msg = append([]kafka.Message(nil), msgs...)
	return f.err
}

type fakeHTTPPublisher struct {
	called bool
	event  *Event
	err    error
}

func (f *fakeHTTPPublisher) Publish(ctx context.Context, event *Event) error {
	f.called = true
	f.event = event
	return f.err
}

func TestKafkaPublisher_PublishesMappedTopicToKafka(t *testing.T) {
	fallback := &fakeHTTPPublisher{}
	var gotTopic string
	var gotAck kafka.RequiredAcks
	writer := &fakeKafkaWriter{}

	publisher := &KafkaPublisher{
		brokers:      []string{"broker-1:9092"},
		fallback:     fallback,
		topicMapping: map[string]string{"billing.created": "billing-events"},
		topicAcks:    map[string]kafka.RequiredAcks{"billing-events": kafka.RequireOne},
		writerFactory: func(brokers []string, topic string, ack kafka.RequiredAcks) kafkaWriter {
			gotTopic = topic
			gotAck = ack
			return writer
		},
	}

	eventData := EventData{Type: "billing.created", Data: map[string]string{"invoice": "123"}}
	dataBytes, err := json.Marshal(eventData)
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), &Event{
		ID:        uuid.New(),
		EventType: "billing.created",
		EventData: dataBytes,
	})
	require.NoError(t, err)
	assert.Equal(t, "billing-events", gotTopic)
	assert.Equal(t, kafka.RequireOne, gotAck)
	assert.False(t, fallback.called)
	assert.Len(t, writer.msg, 1)
}

func TestKafkaPublisher_FallsBackToHTTPForUnmappedTopic(t *testing.T) {
	fallback := &fakeHTTPPublisher{}
	publisher := &KafkaPublisher{
		brokers:      []string{"broker-1:9092"},
		fallback:     fallback,
		topicMapping: map[string]string{"billing.created": "billing-events"},
		writerFactory: func(brokers []string, topic string, ack kafka.RequiredAcks) kafkaWriter {
			return &fakeKafkaWriter{}
		},
	}

	dataBytes, err := json.Marshal(EventData{Type: "other"})
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), &Event{EventType: "other", EventData: dataBytes})
	require.NoError(t, err)
	assert.True(t, fallback.called)
}

func TestKafkaPublisher_ReturnsErrorWhenWriterFails(t *testing.T) {
	fallback := &fakeHTTPPublisher{}
	publisher := &KafkaPublisher{
		brokers:      []string{"broker-1:9092"},
		fallback:     fallback,
		topicMapping: map[string]string{"billing.created": "billing-events"},
		writerFactory: func(brokers []string, topic string, ack kafka.RequiredAcks) kafkaWriter {
			return &fakeKafkaWriter{err: errors.New("broker unavailable")}
		},
	}

	dataBytes, err := json.Marshal(EventData{Type: "billing.created"})
	require.NoError(t, err)

	err = publisher.Publish(context.Background(), &Event{EventType: "billing.created", EventData: dataBytes})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker unavailable")
	assert.False(t, fallback.called)
}

func TestParseKafkaConfigFromEnv(t *testing.T) {
	t.Setenv("OUTBOX_KAFKA_BROKERS", "broker-1:9092,broker-2:9092")
	t.Setenv("OUTBOX_KAFKA_TOPIC_MAP", "billing.created=billing-events")
	t.Setenv("OUTBOX_KAFKA_ACKS", "1")
	t.Setenv("OUTBOX_KAFKA_TOPIC_ACKS", "billing-events=all")

	cfg, err := parseKafkaConfigFromEnv()
	require.NoError(t, err)
	assert.Equal(t, []string{"broker-1:9092", "broker-2:9092"}, cfg.brokers)
	assert.Equal(t, map[string]string{"billing.created": "billing-events"}, cfg.topicMapping)
	assert.Equal(t, kafka.RequireOne, cfg.defaultAck)
	assert.Equal(t, kafka.RequireAll, cfg.topicAcks["billing-events"])
}
