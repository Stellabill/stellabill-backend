package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MemorySink implementation for testing
type MemorySink struct {
	entries []AuditEvent
}

func (m *MemorySink) WriteEvent(e AuditEvent) error {
	m.entries = append(m.entries, e)
	return nil
}

func (m *MemorySink) Entries() []AuditEvent {
	return m.entries
}

func TestLoggerRedactsSensitiveMetadata(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	_, err := logger.Log(context.Background(), AuditEvent{
		Actor:    "alice",
		Action:   "auth_failure",
		Resource: "/login",
		Outcome:  "denied",
		Metadata: map[string]interface{}{
			"password":      "super-secret",
			"token":         "abcd",
			"note":          "safe",
			"Authorization": "Bearer abc",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}

	meta := entries[0].Metadata
	if meta["password"] != redactedValue || meta["token"] != redactedValue || meta["Authorization"] != redactedValue {
		t.Fatalf("expected sensitive fields to be redacted, got %#v", meta)
	}

	if meta["note"] != "safe" {
		t.Fatalf("expected non-sensitive field to remain, got %#v", meta)
	}
}

func TestLoggerChainsHashes(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	first, err := logger.Log(context.Background(), AuditEvent{
		Actor:    "alice",
		Action:   "admin_action",
		Resource: "/admin",
		Outcome:  "success",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := logger.Log(context.Background(), AuditEvent{
		Actor:    "bob",
		Action:   "retry",
		Resource: "/admin",
		Outcome:  "partial",
		Metadata: map[string]interface{}{"attempt": 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if second.PrevHash != first.Hash {
		t.Fatalf("hash chain broken: prev=%s current=%s", second.PrevHash, first.Hash)
	}
}

func TestLoggerUsesContextActor(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	ctx := WithActor(context.Background(), "context-actor")
	// Passing empty Actor in struct to trigger context fallback
	_, err := logger.Log(ctx, AuditEvent{Action: "action", Resource: "/x", Outcome: "ok"})
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sink.Entries()[0].Actor != "context-actor" {
		t.Fatalf("expected actor from context, got %s", sink.Entries()[0].Actor)
	}
}

func TestRedactsSensitiveLookingValues(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	_, err := logger.Log(context.Background(), AuditEvent{
		Metadata: map[string]interface{}{"note": "Bearer abcdef"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sink.Entries()[0].Metadata["note"] != redactedValue {
		t.Fatalf("expected bearer token value to be redacted, got %#v", sink.Entries()[0].Metadata)
	}
}

func TestFileSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	sink := NewFileSink(path)
	logger := NewLogger("secret", sink)

	_, err := logger.Log(context.Background(), AuditEvent{
		Actor:  "alice",
		Action: "auth_failure",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "\"action\":\"auth_failure\"") {
		t.Fatalf("entry not written as jsonl: %s", content)
	}
}

func TestLoggerHandlesNilReceiverAndNilSink(t *testing.T) {
	var logger *Logger
	_, err := logger.Log(context.Background(), AuditEvent{})
	if err == nil {
		t.Fatal("expected error for nil logger")
	}

	if NewLogger("secret", nil) != nil {
		t.Fatal("expected nil logger when sink is nil")
	}
}
