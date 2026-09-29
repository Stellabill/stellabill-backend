package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type auditContextKey string

const (
	actorKey auditContextKey = "audit_actor"
)

// WithActor returns a new context with the provided actor ID.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

// FromContext extracts the actor ID from the context.
func FromContext(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(actorKey).(string)
	return val, ok
}

type Logger struct {
	mu     sync.Mutex
	secret []byte
	sink   Sink
}

func NewLogger(secret string, sink Sink) *Logger {
	if sink == nil {
		return nil
	}
	s := secret
	if s == "" {
		s = "default-stellabill-internal-secret" // Fallback for dev
	}
	return &Logger{
		secret: []byte(s),
		sink:   sink,
	}
}

// NewSinkFromEnv creates a sink from AUDIT_LOG_PATH, falling back to stderr.
func NewSinkFromEnv() Sink {
	path := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH"))
	if path == "" {
		return NewStderrSink()
	}
	return NewFileSink(path)
}

func (l *Logger) Log(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	if l == nil {
		return AuditEvent{}, errors.New("audit logger is not initialized")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 1. Prepare Event Metadata
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}

	// 2. Redaction (PII Protection)
	event.Metadata = l.redact(event.Metadata)

	// 3. Persistence (Hashing is delegated to the Sink)
	if err := l.sink.WriteEvent(&event); err != nil {
		return AuditEvent{}, fmt.Errorf("failed to write to sink: %w", err)
	}

	return event, nil
}

const redactedValue = "[REDACTED]"

func (l *Logger) redact(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}

	sensitiveKeys := []string{"password", "token", "secret", "auth", "key", "cvv", "card"}
	newMeta := make(map[string]interface{})

	for k, v := range meta {
		valStr := strings.ToLower(fmt.Sprintf("%v", v))
		isSensitive := false

		for _, sk := range sensitiveKeys {
			if strings.Contains(strings.ToLower(k), sk) || strings.Contains(valStr, "bearer") {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			newMeta[k] = redactedValue
		} else {
			newMeta[k] = v
		}
	}
	return newMeta
}
