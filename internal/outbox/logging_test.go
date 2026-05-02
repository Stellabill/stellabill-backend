package outbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"stellarbill-backend/internal/structuredlog"
)

func TestDefaultHTTPClientPostDoesNotLogPayloadBody(t *testing.T) {
	previous := defaultLogger
	defer func() { defaultLogger = previous }()

	var buf bytes.Buffer
	defaultLogger = structuredlog.New(&buf)

	client := &DefaultHTTPClient{}
	payload := []byte(`{"token":"super-secret","email":"alice@example.com"}`)
	_, err := client.Post("https://example.com/hooks", "application/json", payload)
	if err != nil {
		t.Fatalf("post returned error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "super-secret") || strings.Contains(output, "alice@example.com") {
		t.Fatalf("expected payload values to stay out of logs, got %q", output)
	}
	if !strings.Contains(output, `"payload_bytes":`+strconv.Itoa(len(payload))) {
		t.Fatalf("expected payload size metadata in logs, got %q", output)
	}
}

func TestDispatcherLogFailureThrottlesRepeatedErrors(t *testing.T) {
	dispatcher := NewDispatcher(nil, nil, DefaultDispatcherConfig()).(*dispatcher)
	dispatcher.throttler = structuredlog.NewThrottler(40 * time.Millisecond)

	var buf bytes.Buffer
	dispatcher.logger = structuredlog.New(&buf)

	fields := structuredlog.Fields{
		structuredlog.FieldRequestID: "",
		structuredlog.FieldActor:     "system",
		structuredlog.FieldTenant:    "system",
		structuredlog.FieldRoute:     "outbox.dispatcher.retry",
		structuredlog.FieldStatus:    "retry_scheduled",
		structuredlog.FieldDuration:  0,
	}

	dispatcher.logFailure("db_down", "failed to fetch pending outbox events", errors.New("db down"), fields)
	dispatcher.logFailure("db_down", "failed to fetch pending outbox events", errors.New("db down"), fields)
	time.Sleep(50 * time.Millisecond)
	dispatcher.logFailure("db_down", "failed to fetch pending outbox events", errors.New("db down"), fields)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected first log and one summary log, got %d lines: %q", len(lines), buf.String())
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &summary); err != nil {
		t.Fatalf("unmarshal summary log: %v", err)
	}
	if int(summary["suppressed_count"].(float64)) != 1 {
		t.Fatalf("suppressed_count = %v, want 1", summary["suppressed_count"])
	}
}
