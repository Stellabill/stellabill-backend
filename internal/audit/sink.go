package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// FileSink appends JSONL audit entries to a file path.
type FileSink struct {
	mu       sync.Mutex
	path     string
	lastHash string
}

// NewFileSink returns a sink that writes to the provided path (default: audit.log).
func NewFileSink(path string) *FileSink {
	if path == "" {
		path = "audit.log"
	}
	sink := &FileSink{path: path}
	_ = sink.recoverLastHash() // Recover hash if file exists, ignore errors for now (WriteEvent will handle)
	return sink
}

func (s *FileSink) recoverLastHash() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lastEntry AuditEvent
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry AuditEvent
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		lastEntry = entry
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if lastEntry.Hash != "" {
		s.lastHash = lastEntry.Hash
	}
	return nil
}

func (s *FileSink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.recoverLastHash(); err != nil {
		return err
	}

	stat, err := os.Stat(s.path)
	if err == nil && stat.Size() > 0 && s.lastHash == "" {
		return errors.New("failed to recover previous hash from non-empty file")
	}

	e.PrevHash = s.lastHash
	e.Hash = computeEventHash(e)

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

// Verify checks the integrity of the audit log file.
func Verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var prevHash string
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		lineNum++
		if len(line) == 0 {
			continue
		}

		var entry AuditEvent
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}

		if entry.PrevHash != prevHash {
			return fmt.Errorf("invalid prev_hash at line %d", lineNum)
		}

		computedHash := computeEventHash(entry)
		if entry.Hash != computedHash {
			return fmt.Errorf("invalid hash at line %d", lineNum)
		}

		prevHash = entry.Hash
	}

	return scanner.Err()
}

func computeEventHash(e AuditEvent) string {
	type tempEntry struct {
		Timestamp time.Time              `json:"timestamp"`
		RequestID string                 `json:"request_id"`
		Actor     string                 `json:"actor"`
		Action    string                 `json:"action"`
		Resource  string                 `json:"resource"`
		Outcome   string                 `json:"outcome"`
		PrevHash  string                 `json:"prev_hash,omitempty"`
		Metadata  map[string]interface{} `json:"metadata,omitempty"`
	}

	temp := tempEntry{
		Timestamp: e.Timestamp,
		RequestID: e.RequestID,
		Actor:     e.Actor,
		Action:    e.Action,
		Resource:  e.Resource,
		Outcome:   e.Outcome,
		PrevHash:  e.PrevHash,
		Metadata:  sortMap(e.Metadata),
	}

	canonical, err := json.Marshal(temp)
	if err != nil {
		panic(err)
	}

	hash := sha256.New()
	hash.Write(canonical)
	return hex.EncodeToString(hash.Sum(nil))
}

func sortMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]interface{}, len(m))
	for _, k := range keys {
		v := m[k]
		if nested, ok := v.(map[string]interface{}); ok {
			sorted[k] = sortMap(nested)
		} else {
			sorted[k] = v
		}
	}
	return sorted
}

// StderrSink writes JSONL audit entries to os.Stderr.
type StderrSink struct {
	mu sync.Mutex
}

// NewStderrSink returns a sink that writes to stderr.
func NewStderrSink() *StderrSink {
	return &StderrSink{}
}

func (s *StderrSink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	encoded, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = os.Stderr.Write(append(encoded, '\n'))
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
