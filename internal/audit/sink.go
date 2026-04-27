package audit

import (
	"encoding/json"
	"os"
	"sync"
)

// FileSink appends JSONL audit entries to a file path.
type FileSink struct {
	mu   sync.Mutex
	path string
}

// NewFileSink returns a sink that writes to the provided path (default: audit.log).
func NewFileSink(path string) *FileSink {
	if path == "" {
		path = "audit.log"
	}
	return &FileSink{path: path}
}

func (s *FileSink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	encoded, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(encoded, '\n'))
	return err
}

// MemorySink keeps audit entries in-memory, intended for tests.
type MemorySink struct {
	mu      sync.Mutex
	entries []AuditEvent
}

// WriteEvent satisfies the Sink interface.
func (s *MemorySink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

// Entries returns a copy of stored entries.
func (s *MemorySink) Entries() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.entries))
	copy(out, s.entries)
	return out
}
