package cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// KafkaSink writes CDC changes to Kafka topics.
//
// Topic naming convention: changes are written to a topic derived
// from "{prefix}.{schema}.{table}" (e.g. "stellabill.public.plans").
//
// Dependencies: this sink requires a Kafka producer implementation.
// It uses a MessageWriter interface so callers can plug in any Kafka
// client (segmentio/kafka-go, confluent-kafka-go, sarama, etc.).
type KafkaSink struct {
	writer    MessageWriter
	topicPref string

	mu     sync.Mutex
	closed bool
}

// MessageWriter abstracts a Kafka producer so the sink is not coupled
// to a specific Kafka library.
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs []Message) error
	Flush(ctx context.Context) error
	Close() error
}

// Message is a Kafka message to be written.
type Message struct {
	Topic string
	Key   []byte
	Value []byte
}

// KafkaSinkConfig configures the Kafka sink.
type KafkaSinkConfig struct {
	Writer      MessageWriter
	TopicPrefix string
}

// NewKafkaSink creates a new Kafka sink.
func NewKafkaSink(cfg KafkaSinkConfig) (*KafkaSink, error) {
	if cfg.Writer == nil {
		return nil, fmt.Errorf("cdc: KafkaSink requires a MessageWriter")
	}
	if cfg.TopicPrefix == "" {
		cfg.TopicPrefix = "stellabill"
	}
	return &KafkaSink{
		writer:    cfg.Writer,
		topicPref: cfg.TopicPrefix,
	}, nil
}

func (s *KafkaSink) topicFor(schema, table string) string {
	return fmt.Sprintf("%s.%s.%s", s.topicPref, schema, table)
}

// WriteChange marshals the change to JSON and writes it to Kafka.
func (s *KafkaSink) WriteChange(ctx context.Context, change Change) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errSinkClosed
	}
	s.mu.Unlock()

	value, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("cdc: marshal change: %w", err)
	}

	key := []byte(fmt.Sprintf("%s/%s/%d", change.Schema, change.Table, change.LSN))

	msg := Message{
		Topic: s.topicFor(change.Schema, change.Table),
		Key:   key,
		Value: value,
	}

	if err := s.writer.WriteMessages(ctx, []Message{msg}); err != nil {
		return fmt.Errorf("cdc: kafka write: %w", err)
	}

	return nil
}

// Flush is a no-op for KafkaSink.
func (s *KafkaSink) Flush(_ context.Context) error { return nil }

// Close flushes and closes the underlying Kafka writer.
func (s *KafkaSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	// Flush before closing to ensure buffered messages are sent.
	_ = s.writer.Flush(context.Background())
	return s.writer.Close()
}

// mockMessageWriter is a test double for MessageWriter.
type mockMessageWriter struct {
	mu       sync.Mutex
	messages []Message
	closed   bool
	writeErr error
}

func newMockMessageWriter() *mockMessageWriter {
	return &mockMessageWriter{}
}

func (m *mockMessageWriter) Flush(_ context.Context) error {
	return nil
}

func (m *mockMessageWriter) WriteMessages(_ context.Context, msgs []Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.messages = append(m.messages, msgs...)
	return nil
}

func (m *mockMessageWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockMessageWriter) Messages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// Ensure interface satisfaction at compile time.
var (
	_ Sink          = (*StdoutSink)(nil)
	_ Sink          = (*MemorySink)(nil)
	_ Sink          = (*KafkaSink)(nil)
	_ MessageWriter = (*mockMessageWriter)(nil)
)
