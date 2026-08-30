package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ComputeHash calculates the SHA-256 hash of the canonical JSON representation of the event
func ComputeHash(e AuditEvent) string {
	eCopy := e
	eCopy.Hash = "" // exclude the hash field itself
	encoded, _ := json.Marshal(eCopy)
	h := sha256.Sum256(encoded)
	return hex.EncodeToString(h[:])
}

// FileSink appends JSONL audit entries to a file path.
type FileSink struct {
	mu       sync.Mutex
	path     string
	lastHash string
	init     bool
}

// NewFileSink returns a sink that writes to the provided path (default: audit.log).
func NewFileSink(path string) *FileSink {
	if path == "" {
		path = "audit.log"
	}
	return &FileSink{path: path}
}

func (s *FileSink) loadLastHashLocked() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.init = true
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lastLine string
	for scanner.Scan() {
		if text := scanner.Text(); text != "" {
			lastLine = text
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if lastLine != "" {
		var lastEvent AuditEvent
		if err := json.Unmarshal([]byte(lastLine), &lastEvent); err != nil {
			return fmt.Errorf("failed to parse last event: %w", err)
		}
		s.lastHash = lastEvent.Hash
	}
	s.init = true
	return nil
}

func (s *FileSink) WriteEvent(e *AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.init {
		if err := s.loadLastHashLocked(); err != nil {
			return err
		}
	}

	e.PrevHash = s.lastHash
	e.Hash = ComputeHash(*e)

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
	if err != nil {
		return err
	}

	s.lastHash = e.Hash
	return nil
}

// MemorySink keeps audit entries in-memory, intended for tests.
type MemorySink struct {
	mu       sync.Mutex
	entries  []AuditEvent
	lastHash string
}

// WriteEvent satisfies the Sink interface.
func (s *MemorySink) WriteEvent(e *AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e.PrevHash = s.lastHash
	e.Hash = ComputeHash(*e)
	s.lastHash = e.Hash

	s.entries = append(s.entries, *e)
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

// Verify reads the audit log at path and validates the hash chain.
func Verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var expectedPrev string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var e AuditEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
		}

		if e.PrevHash != expectedPrev {
			return fmt.Errorf("line %d: broken chain, expected prev_hash %q, got %q", lineNum, expectedPrev, e.PrevHash)
		}

		computed := ComputeHash(e)
		if computed != e.Hash {
			return fmt.Errorf("line %d: hash mismatch, expected %q, got %q", lineNum, e.Hash, computed)
		}

		expectedPrev = e.Hash
	}
	return scanner.Err()
}
