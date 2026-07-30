package cdc

import (
	"context"
	"encoding/json"
	"log"
)

// StdoutSink writes CDC changes to stdout as JSON lines.
// This sink is intended for development, debugging, and integration
// testing. It is NOT suitable for production use because:
//   - There is no durability guarantee.
//   - There is no back-pressure – if the consumer is faster than stdout
//     the process memory will grow unbounded.
type StdoutSink struct{}

// NewStdoutSink creates a new stdout sink.
func NewStdoutSink() *StdoutSink {
	return &StdoutSink{}
}

// WriteChange marshals the change to JSON and prints it to stdout.
func (s *StdoutSink) WriteChange(_ context.Context, change Change) error {
	data, err := json.Marshal(change)
	if err != nil {
		return err
	}
	log.Printf("cdc: %s", string(data))
	return nil
}

// Flush is a no-op for stdout.
func (s *StdoutSink) Flush(_ context.Context) error { return nil }

// Close is a no-op for stdout.
func (s *StdoutSink) Close() error { return nil }
