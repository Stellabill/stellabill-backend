package structuredlog

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestLoggerRedactsSensitiveFieldsAndMessages(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)
	logger.now = func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	}

	logger.Error("token=abc123 user=alice@example.com", Fields{
		FieldRequestID: "req-123",
		"authorization": "Bearer abc.def.ghi",
		"actor":         "alice@example.com",
		"tenant":        "tenant-1",
		"route":         "/api/test",
		"status":        500,
		"duration_ms":   12,
	})

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	if entry["level"] != "error" {
		t.Fatalf("level = %v, want error", entry["level"])
	}
	if entry["authorization"] != redactedValue {
		t.Fatalf("authorization = %v, want redacted", entry["authorization"])
	}
	if entry["actor"] != redactedValue {
		t.Fatalf("actor = %v, want redacted email", entry["actor"])
	}
	if entry["message"] != "token=[REDACTED] user=[REDACTED]" {
		t.Fatalf("message = %v", entry["message"])
	}
}

func TestThrottlerSuppressesRepeatedFailuresWithinWindow(t *testing.T) {
	current := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	throttler := NewThrottler(10 * time.Second)
	throttler.now = func() time.Time { return current }

	first := throttler.Decide("db_down")
	if !first.Allow || first.Suppressed != 0 {
		t.Fatalf("unexpected first decision: %+v", first)
	}

	second := throttler.Decide("db_down")
	if second.Allow {
		t.Fatalf("second decision should be suppressed: %+v", second)
	}

	third := throttler.Decide("db_down")
	if third.Allow {
		t.Fatalf("third decision should be suppressed: %+v", third)
	}

	current = current.Add(11 * time.Second)
	next := throttler.Decide("db_down")
	if !next.Allow {
		t.Fatalf("expected summary log after window reset: %+v", next)
	}
	if next.Suppressed != 2 {
		t.Fatalf("suppressed = %d, want 2", next.Suppressed)
	}
}
