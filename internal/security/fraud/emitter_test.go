package fraud

import (
	"strings"
	"testing"
	"time"

	"stellarbill-backend/internal/audit"
)

func TestAdapt_NilLogger(t *testing.T) {
	if Adapt(nil) != nil {
		t.Fatal("Adapt(nil) should return nil emitter")
	}
}

func TestAuditEmitter_EmitsCanonicalEvent(t *testing.T) {
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	em := Adapt(logger)
	if em == nil {
		t.Fatal("expected non-nil emitter")
	}

	detectedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := em.Emit(AuditEvent{
		Action:     AuditAction,
		Signal:     SignalAuthFailRate,
		TenantHash: "deadbeef",
		Count:      42,
		Threshold:  20,
		Window:     time.Minute.String(),
		DetectedAt: detectedAt,
	}); err != nil {
		t.Fatalf("emit error: %v", err)
	}

	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Action != AuditAction {
		t.Fatalf("action = %q, want %q", e.Action, AuditAction)
	}
	if e.Actor != "deadbeef" {
		t.Fatalf("actor = %q, want tenant hash", e.Actor)
	}
	if e.Resource != string(SignalAuthFailRate) {
		t.Fatalf("resource = %q, want signal name", e.Resource)
	}
	if e.Outcome != "flagged" {
		t.Fatalf("outcome = %q, want flagged", e.Outcome)
	}
	if e.Metadata["count"] != int64(42) {
		t.Fatalf("metadata count = %v, want 42", e.Metadata["count"])
	}
	if e.Hash == "" {
		t.Fatal("audit logger should have chained a hash")
	}
}

// TestEmit_EndToEndNoPII wires a real collector to the audit logger and asserts
// the raw tenant identifier never appears in persisted audit output.
func TestEmit_EndToEndNoPII(t *testing.T) {
	clk := newMockClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	c := NewCollector(testConfig(), Adapt(logger), WithClock(clk))

	const rawTenant = "tenant-acme-secret-id"
	for i := 0; i < 3; i++ {
		c.Observe(rawTenant, SignalAuthFailRate)
	}

	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	for _, e := range entries {
		if strings.Contains(e.Actor, rawTenant) {
			t.Fatalf("raw tenant id leaked into actor: %q", e.Actor)
		}
		for k, v := range e.Metadata {
			if s, ok := v.(string); ok && strings.Contains(s, rawTenant) {
				t.Fatalf("raw tenant id leaked into metadata[%s]: %q", k, s)
			}
		}
	}
}
