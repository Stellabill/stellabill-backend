package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const redactedValue = "***REDACTED***"

type Logger struct {
	mu       sync.Mutex
	secret   []byte
	sink     Sink
	lastHash string
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

func (l *Logger) Log(ctx context.Context, event AuditEvent) (AuditEvent, error) {
	if l == nil {
		return AuditEvent{}, errors.New("audit logger is not initialized")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 1. Prepare Event Metadata
	event.Timestamp = event.Timestamp.UTC()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Actor == "" {
		event.Actor = GetActor(ctx)
	}
	
	// 2. Redaction (PII Protection)
	event.Metadata = l.redact(event.Metadata)

	// 3. Cryptographic Chaining
	event.PrevHash = l.lastHash
	event.Hash = l.computeHash(event)
	l.lastHash = event.Hash

	// 4. Persistence
	if err := l.sink.WriteEvent(event); err != nil {
		return AuditEvent{}, fmt.Errorf("failed to write to sink: %w", err)
	}

	return event, nil
}

func (l *Logger) computeHash(e AuditEvent) string {
	// Create a stable string representation for hashing
	raw := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%v", 
		e.Timestamp.Unix(), e.Actor, e.Action, e.Resource, e.Outcome, e.PrevHash, e.Metadata)
	
	h := hmac.New(sha256.New, l.secret)
	h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil))
}

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

func (l *Logger) LastHash() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastHash
}
