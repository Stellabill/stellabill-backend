package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerRedactsSensitiveMetadata(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	_, err := logger.Log(context.Background(), "alice", "auth_failure", "/login", "denied", map[string]string{
		"password":      "super-secret",
		"token":         "abcd",
		"note":          "safe",
		"Authorization": "Bearer abc",
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

	first, err := logger.Log(context.Background(), "alice", "admin_action", "/admin", "success", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := logger.Log(context.Background(), "bob", "retry", "/admin", "partial", map[string]string{"attempt": "2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if second.PrevHash != first.Hash {
		t.Fatalf("hash chain broken: prev=%s current=%s", second.PrevHash, first.Hash)
	}
	if logger.LastHash() != second.Hash {
		t.Fatalf("logger did not record last hash")
	}
}

func TestLoggerUsesContextActor(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	ctx := WithActor(context.Background(), "context-actor")
	_, err := logger.Log(ctx, "", "action", "/x", "ok", nil)
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

	_, err := logger.Log(context.Background(), "alice", "action", "/x", "ok", map[string]string{
		"note": "Bearer abcdef",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink.Entries()[0].Metadata["note"] != redactedValue {
		t.Fatalf("expected bearer token to be redacted, got %#v", sink.Entries()[0].Metadata)
	}
}

func TestFileSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	sink := NewFileSink(path)
	logger := NewLogger("secret", sink)

	_, err := logger.Log(context.Background(), "alice", "auth_failure", "/login", "denied", nil)
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

func TestNewLoggerDefaultsAndFileSinkDefaultPath(t *testing.T) {
	originalWD, _ := os.Getwd()
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(originalWD)

	logger := NewLogger("", NewFileSink(""))
	if logger == nil {
		t.Fatal("logger should not be nil even with empty secret")
	}
	if _, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil); err != nil {
		t.Fatalf("log failed: %v", err)
	}
	if _, err := os.Stat("audit.log"); err != nil {
		t.Fatalf("default audit log file not created: %v", err)
	}
}

func TestLoggerHandlesNilReceiverAndNilSink(t *testing.T) {
	var logger *Logger
	if _, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil); err == nil {
		t.Fatal("expected error for nil logger")
	}
	if NewLogger("secret", nil) != nil {
		t.Fatal("expected nil logger when sink is nil")
	}
}

func TestLoggerRedactEmptyMetadata(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)
	_, _ = logger.Log(context.Background(), "a", "b", "c", "d", map[string]string{})
	if sink.Entries()[0].Metadata != nil {
		t.Fatal("expected nil metadata for empty map")
	}
}

func TestRedactsBasicAuth(t *testing.T) {
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)
	_, _ = logger.Log(context.Background(), "a", "b", "c", "d", map[string]string{"h": "Basic secret"})
	if sink.Entries()[0].Metadata["h"] != redactedValue {
		t.Fatal("expected Basic auth to be redacted")
	}
}

