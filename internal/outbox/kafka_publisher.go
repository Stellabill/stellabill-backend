package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

type kafkaWriterFactory func(brokers []string, topic string, ack kafka.RequiredAcks) kafkaWriter

type kafkaPublisherConfig struct {
	brokers      []string
	topicMapping map[string]string
	defaultAck   kafka.RequiredAcks
	topicAcks    map[string]kafka.RequiredAcks
}

// KafkaPublisher routes selected outbox topics to Kafka while preserving an
// HTTP fallback for unconfigured topics.
type KafkaPublisher struct {
	brokers       []string
	topicMapping  map[string]string
	topicAcks     map[string]kafka.RequiredAcks
	defaultAck    kafka.RequiredAcks
	writerFactory kafkaWriterFactory
	fallback      Publisher
}

func NewKafkaPublisher(brokers []string, topicMapping map[string]string, fallback Publisher) *KafkaPublisher {
	return &KafkaPublisher{
		brokers:       append([]string(nil), brokers...),
		topicMapping:  cloneTopicMapping(topicMapping),
		topicAcks:     map[string]kafka.RequiredAcks{},
		defaultAck:    kafka.RequireOne,
		writerFactory: newKafkaWriter,
		fallback:      fallback,
	}
}

func NewKafkaPublisherFromEnv(fallback Publisher) (*KafkaPublisher, error) {
	cfg, err := parseKafkaConfigFromEnv()
	if err != nil {
		return nil, err
	}

	pub := NewKafkaPublisher(cfg.brokers, cfg.topicMapping, fallback)
	pub.defaultAck = cfg.defaultAck
	pub.topicAcks = cfg.topicAcks
	return pub, nil
}

func (p *KafkaPublisher) Publish(ctx context.Context, event *Event) error {
	if event == nil {
		return fmt.Errorf("kafka publisher received nil event")
	}

	topic, ok := p.resolveTopic(event)
	if !ok {
		if p.fallback != nil {
			return p.fallback.Publish(ctx, event)
		}
		return nil
	}

	if len(p.brokers) == 0 {
		return fmt.Errorf("kafka publisher has no brokers configured")
	}

	writer := p.writerFactory(p.brokers, topic, p.resolveAck(topic))
	if writer == nil {
		return fmt.Errorf("kafka publisher writer factory returned nil")
	}

	payload := map[string]interface{}{
		"id":             event.ID,
		"type":           event.EventType,
		"data":           event.EventData,
		"occurred_at":    event.OccurredAt,
		"aggregate_id":   safeString(event.AggregateID),
		"aggregate_type": safeString(event.AggregateType),
		"version":        event.Version,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal kafka payload: %w", err)
	}

	start := time.Now()
	err = writer.WriteMessages(ctx, kafka.Message{Key: []byte(event.ID.String()), Value: body})
	latency := time.Since(start).Seconds()
	if OutboxKafkaProduceLatency != nil {
		OutboxKafkaProduceLatency.WithLabelValues(topic).Observe(latency)
	}
	if err != nil {
		if OutboxKafkaErrorsTotal != nil {
			OutboxKafkaErrorsTotal.WithLabelValues(topic, "publish_error").Inc()
		}
		return fmt.Errorf("kafka publish failed for topic %s: %w", topic, err)
	}

	return nil
}

func (p *KafkaPublisher) resolveTopic(event *Event) (string, bool) {
	if event == nil {
		return "", false
	}
	if topic, ok := p.topicMapping[event.EventType]; ok && topic != "" {
		return topic, true
	}
	if len(event.EventData) == 0 {
		return "", false
	}
	var eventData EventData
	if err := json.Unmarshal(event.EventData, &eventData); err == nil && eventData.Type != "" {
		if topic, ok := p.topicMapping[eventData.Type]; ok && topic != "" {
			return topic, true
		}
	}
	return "", false
}

func (p *KafkaPublisher) resolveAck(topic string) kafka.RequiredAcks {
	if ack, ok := p.topicAcks[topic]; ok {
		return ack
	}
	return p.defaultAck
}

func newKafkaWriter(brokers []string, topic string, ack kafka.RequiredAcks) kafkaWriter {
	return &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		RequiredAcks:           ack,
		Compression:            kafka.Snappy,
		WriteTimeout:           10 * time.Second,
		ReadTimeout:            10 * time.Second,
		AllowAutoTopicCreation: true,
	}
}

func parseKafkaConfigFromEnv() (kafkaPublisherConfig, error) {
	cfg := kafkaPublisherConfig{
		defaultAck: kafka.RequireOne,
		topicAcks:  map[string]kafka.RequiredAcks{},
	}

	for _, broker := range strings.Split(os.Getenv("OUTBOX_KAFKA_BROKERS"), ",") {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			cfg.brokers = append(cfg.brokers, broker)
		}
	}

	if topicMap := os.Getenv("OUTBOX_KAFKA_TOPIC_MAP"); topicMap != "" {
		cfg.topicMapping = map[string]string{}
		for _, entry := range strings.Split(topicMap, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				return kafkaPublisherConfig{}, fmt.Errorf("invalid OUTBOX_KAFKA_TOPIC_MAP entry %q", entry)
			}
			cfg.topicMapping[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	if ackValue := strings.TrimSpace(os.Getenv("OUTBOX_KAFKA_ACKS")); ackValue != "" {
		ack, err := parseKafkaAck(ackValue)
		if err != nil {
			return kafkaPublisherConfig{}, err
		}
		cfg.defaultAck = ack
	}

	if ackValue := strings.TrimSpace(os.Getenv("OUTBOX_KAFKA_TOPIC_ACKS")); ackValue != "" {
		for _, entry := range strings.Split(ackValue, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) != 2 {
				return kafkaPublisherConfig{}, fmt.Errorf("invalid OUTBOX_KAFKA_TOPIC_ACKS entry %q", entry)
			}
			ack, err := parseKafkaAck(strings.TrimSpace(parts[1]))
			if err != nil {
				return kafkaPublisherConfig{}, err
			}
			cfg.topicAcks[strings.TrimSpace(parts[0])] = ack
		}
	}

	return cfg, nil
}

func parseKafkaAck(value string) (kafka.RequiredAcks, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "none", "no_ack":
		return kafka.RequireNone, nil
	case "1", "one", "requireone":
		return kafka.RequireOne, nil
	case "all", "requireall", "-1":
		return kafka.RequireAll, nil
	default:
		return kafka.RequireOne, fmt.Errorf("unsupported kafka ack value %q", value)
	}
}

func cloneTopicMapping(mapping map[string]string) map[string]string {
	if len(mapping) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(mapping))
	for key, value := range mapping {
		cloned[key] = value
	}
	return cloned
}
