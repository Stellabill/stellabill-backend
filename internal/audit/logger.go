package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// redactedValue is applied to sensitive metadata fields.
const redactedValue = "[REDACTED]"

// Entry represents a single audit log record.
type Entry struct {
	Timestamp time.Time         `json:"ts"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Target    string            `json:"target,omitempty"`
	Outcome   string            `json:"outcome"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	PrevHash  string            `json:"prev_hash,omitempty"`
	Hash      string            `json:"hash"`
}

// Sink writes audit entries to a persistence layer (file, stdout, buffer, etc).
type Sink interface {
	WriteEntry(Entry) error
}

// Logger creates tamper-evident audit records by chaining HMAC hashes between entries.
type Logger struct {
	mu       sync.Mutex
	secret   []byte
	sink     Sink
	lastHash string
}

// NewLogger builds a logger that writes to the provided sink. A non-empty secret is required
// for HMAC chaining. When secret is empty a deterministic fallback is used to avoid silent nils.
func NewLogger(secret string, sink Sink) *Logger {
	if sink == nil {
		return nil
	}
	if secret == "" {
		secret = "stellarbill-dev-audit"
	}
	return &Logger{
		secret: []byte(secret),
		sink:   sink,
	}
}

// Log writes a tamper-evident entry to the sink. The returned entry contains the final hash.
func (l *Logger) Log(ctx context.Context, actor, action, target, outcome string, metadata map[string]string) (Entry, error) {
	if l == nil {
		return Entry{}, errors.New("audit logger is not initialized")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := Entry{
		Timestamp: time.Now().UTC(),
		Actor:     strings.TrimSpace(fallbackActor(ctx, actor)),
		Action:    strings.TrimSpace(action),
		Target:    strings.TrimSpace(target),
		Outcome:   strings.TrimSpace(outcome),
		Metadata:  redact(metadata),
		PrevHash:  l.lastHash,
	}

	entry.Hash = l.computeHash(entry)
	l.lastHash = entry.Hash

	if err := l.sink.WriteEntry(entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// LastHash returns the most recent hash in the chain (useful for integrity checks in tests).
func (l *Logger) LastHash() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastHash
}

func (l *Logger) computeHash(entry Entry) string {
	clone := entry
	clone.Hash = ""
	payload, _ := json.Marshal(clone)

	mac := hmac.New(sha256.New, l.secret)
	mac.Write([]byte(entry.PrevHash))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func fallbackActor(ctx context.Context, actor string) string {
	if trimmed := strings.TrimSpace(actor); trimmed != "" {
		return trimmed
	}
	if ctx != nil {
		if v := ctx.Value(actorContextKey{}); v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return "anonymous"
}

// actorContextKey is used when an upstream authenticator wants to seed the actor identity.
type actorContextKey struct{}

// WithActor annotates a context with the actor performing the action.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, strings.TrimSpace(actor))
}

func redact(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	sanitized := make(map[string]string, len(meta))
	for k, v := range meta {
		lower := strings.ToLower(k)
		if containsSensitiveKey(lower) || looksSensitiveValue(v) {
			sanitized[k] = redactedValue
			continue
		}
		sanitized[k] = v
	}
	return sanitized
}

func containsSensitiveKey(key string) bool {
	switch {
	case strings.Contains(key, "password"),
		strings.Contains(key, "secret"),
		strings.Contains(key, "token"),
		strings.Contains(key, "authorization"),
		strings.Contains(key, "auth_header"),
		strings.Contains(key, "api_key"):
		return true
	default:
		return false
	}
}

func looksSensitiveValue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return strings.HasPrefix(v, "bearer ") || strings.HasPrefix(v, "basic ")
}

