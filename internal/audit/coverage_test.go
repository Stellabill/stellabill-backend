package audit

import (
	"context"
	"testing"
	"time"
)

func TestCoverage_WithActor_FromContext(t *testing.T) {
	ctx := WithActor(context.Background(), "alice")
	actor, ok := FromContext(ctx)
	if !ok || actor != "alice" {
		t.Fatalf("expected alice, got %q (ok=%v)", actor, ok)
	}
}

func TestCoverage_Logger_LastHash(t *testing.T) {
	l := NewLogger("secret", &MemorySink{})
	_ = l.LastHash()
}

func TestCoverage_NewFileSink_Default(t *testing.T) {
	fs := NewFileSink("")
	if fs == nil {
		t.Fatal("expected file sink")
	}
	fs2 := NewFileSink("/tmp/audit-cov.log")
	if fs2 == nil {
		t.Fatal("expected file sink")
	}
	defer func() {
		_ = (&MemorySink{}).WriteEvent(AuditEvent{})
	}()
}

func TestCoverage_NewLogger_NilSink(t *testing.T) {
	if l := NewLogger("secret", nil); l != nil {
		t.Fatal("expected nil logger with nil sink")
	}
	// Empty secret uses default
	if l := NewLogger("", &MemorySink{}); l == nil {
		t.Fatal("expected non-nil logger with empty secret")
	}
}

func TestCoverage_Logger_Log_NilReceiver(t *testing.T) {
	var l *Logger
	if _, err := l.Log(context.Background(), AuditEvent{}); err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestCoverage_FileSink_WriteEvent(t *testing.T) {
	fs := NewFileSink("/tmp/audit-cov.log")
	if err := fs.WriteEvent(AuditEvent{Actor: "a", Action: "x"}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// Bad path should error
	bad := NewFileSink("/nope/does/not/exist/audit.log")
	if err := bad.WriteEvent(AuditEvent{}); err == nil {
		t.Fatal("expected open error for invalid path")
	}
}

func TestCoverage_Logger_LogWithMetadata(t *testing.T) {
	l := NewLogger("k", &MemorySink{})
	_, err := l.Log(context.Background(), AuditEvent{Actor: "a", Action: "x", Metadata: map[string]interface{}{"password": "p"}})
	if err != nil {
		t.Fatal(err)
	}
}

type erroringSink struct{}

func (erroringSink) WriteEvent(AuditEvent) error { return errSinkBoom }

var errSinkBoom = errSink("boom")

type errSink string

func (e errSink) Error() string { return string(e) }

func TestCoverage_Logger_Log_PresetTimeAndSinkError(t *testing.T) {
	l := NewLogger("k", &MemorySink{})
	// Preset timestamp branch
	_, err := l.Log(context.Background(), AuditEvent{Actor: "a", Action: "x", Timestamp: time.Unix(1700000000, 0)})
	if err != nil {
		t.Fatal(err)
	}

	// Sink error path
	l2 := NewLogger("k", erroringSink{})
	if _, err := l2.Log(context.Background(), AuditEvent{Actor: "a", Action: "x"}); err == nil {
		t.Fatal("expected sink error")
	}
}
