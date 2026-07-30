// Package cdc implements a Change Data Capture consumer that streams
// row-level changes from a PostgreSQL logical replication slot to one or
// more pluggable sinks (Kafka, stdout, etc.).
//
// Architecture at a glance:
//  1. A migration (0014_create_cdc_publication) creates a PUBLICATION that
//     defines which tables and columns are replicated. PII columns are
//     excluded via column-level allowlists.
//  2. The Consumer connects to PostgreSQL using pgx and the pgproto3
//     replication protocol, creates (or reuses) a logical replication
//     slot with the pgoutput plugin, and begins streaming WAL changes.
//  3. The consumer decodes pgoutput binary messages into typed Change
//     events and forwards them to the configured Sink.
//  4. Sinks are pluggable – implementations exist for stdout (debugging)
//     and Kafka (production). A MemorySink is provided for tests.
//
// Slot recovery: if the consumer crashes, it resumes from the last
// confirmed LSN stored by the sink. Kafka sinks store offsets in Kafka
// itself; stdout and MemorySink provide no durability.
package cdc

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errSinkClosed = errors.New("cdc: sink is closed")

// Sink receives decoded CDC changes from the consumer.
// Each call to WriteChange represents one row-level mutation
// (INSERT / UPDATE / DELETE) in commit order.
//
// Implementations must be safe for concurrent use because the
// consumer may fan out to multiple sinks.
type Sink interface {
	// WriteChange is called for every row-level change emitted by the
	// replication stream, in commit order.
	WriteChange(ctx context.Context, change Change) error

	// Flush is called when the consumer catches up and wants to
	// synchronise durable state (e.g. commit Kafka offsets).
	Flush(ctx context.Context) error

	// Close is called during consumer shutdown so the sink can release
	// connections or flush final buffers.
	Close() error
}

// Change represents a single row mutation propagated through the CDC stream.
type Change struct {
	// Schema is the PostgreSQL schema name (typically "public").
	Schema string `json:"schema"`
	// Table is the name of the table that was mutated.
	Table string `json:"table"`
	// Op is the operation: "insert", "update", or "delete".
	Op string `json:"op"`
	// Before contains column values before the mutation (nil for inserts).
	Before map[string]any `json:"before,omitempty"`
	// After contains column values after the mutation (nil for deletes).
	After map[string]any `json:"after,omitempty"`
	// LSN is the Log Sequence Number of this change, used for slot
	// advancement and crash recovery.
	LSN uint64 `json:"lsn"`
	// Timestamp is the commit timestamp of the transaction.
	Timestamp time.Time `json:"timestamp"`
}

// ConsumerConfig configures the CDC consumer.
type ConsumerConfig struct {
	// ConnString is the PostgreSQL connection string (must be a
	// replication-capable user, e.g. with REPLICATION privilege).
	ConnString string

	// SlotName is the name of the logical replication slot. The consumer
	// creates it if it does not exist.
	SlotName string

	// PublicationName is the name of the publication to subscribe to.
	PublicationName string

	// StandbyTimeout configures how often the consumer sends status
	// updates to the server (affects WAL retention).
	StandbyTimeout time.Duration

	// Sinks is the list of sinks that receive decoded changes.
	Sinks []Sink

	// MaxReconnectAttempts limits reconnection retries on error.
	// Zero means no limit.
	MaxReconnectAttempts int

	// ReconnectBackoff is the base duration between reconnection attempts.
	ReconnectBackoff time.Duration
}

// DefaultConsumerConfig returns a reasonable default configuration.
func DefaultConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		SlotName:             "stellabill_cdc_slot",
		PublicationName:      "stellabill_cdc",
		StandbyTimeout:       10 * time.Second,
		MaxReconnectAttempts: 10,
		ReconnectBackoff:     1 * time.Second,
	}
}

// MemorySink stores changes in memory for use in tests.
type MemorySink struct {
	mu      sync.Mutex
	changes []Change
	closed  bool
}

// NewMemorySink creates a new in-memory sink.
func NewMemorySink() *MemorySink {
	return &MemorySink{}
}

// WriteChange appends the change to the in-memory slice.
func (s *MemorySink) WriteChange(_ context.Context, change Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSinkClosed
	}
	s.changes = append(s.changes, change)
	return nil
}

// Flush is a no-op for the memory sink.
func (s *MemorySink) Flush(_ context.Context) error { return nil }

// Close marks the sink as closed.
func (s *MemorySink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// Changes returns a copy of all captured changes.
func (s *MemorySink) Changes() []Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Change, len(s.changes))
	copy(out, s.changes)
	return out
}

// ValidateSink runs a smoke test on a Sink implementation to verify it
// handles the basic contract: Write, Flush, Close.
func ValidateSink(t interface{ Fatal(...any) }, sink Sink) {
	ctx := context.Background()

	err := sink.WriteChange(ctx, Change{
		Schema: "public",
		Table:  "plans",
		Op:     "insert",
		After:  map[string]any{"id": "test-1", "name": "basic"},
		LSN:    1,
	})
	if err != nil {
		t.Fatal("WriteChange failed:", err)
	}

	if err := sink.Flush(ctx); err != nil {
		t.Fatal("Flush failed:", err)
	}

	// Validate double-close is safe.
	_ = sink.Close()
	_ = sink.Close()
}
