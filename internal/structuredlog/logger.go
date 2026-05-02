package structuredlog

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	FieldRequestID = "request_id"
	FieldActor     = "actor"
	FieldTenant    = "tenant"
	FieldRoute     = "route"
	FieldStatus    = "status"
	FieldDuration  = "duration_ms"

	redactedValue = "[REDACTED]"
)

var (
	emailPattern         = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	bearerPattern        = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern           = regexp.MustCompile(`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	secretAssignment     = regexp.MustCompile(`(?i)\b(password|secret|token|jwt|api[_-]?key)\s*[:=]\s*([^\s,;]+)`)
	sensitiveFieldTokens = []string{
		"authorization",
		"auth_header",
		"password",
		"secret",
		"token",
		"jwt",
		"cookie",
		"set_cookie",
		"api_key",
		"apikey",
		"email",
		"phone",
		"ssn",
		"payload",
		"body",
		"event_data",
	}
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Fields map[string]any

type Logger struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

func New(out io.Writer) *Logger {
	if out == nil {
		out = io.Discard
	}
	return &Logger{
		out: out,
		now: time.Now,
	}
}

func (l *Logger) Info(message string, fields Fields) {
	l.Log(LevelInfo, message, fields)
}

func (l *Logger) Warn(message string, fields Fields) {
	l.Log(LevelWarn, message, fields)
}

func (l *Logger) Error(message string, fields Fields) {
	l.Log(LevelError, message, fields)
}

func (l *Logger) Log(level Level, message string, fields Fields) {
	if l == nil {
		return
	}

	entry := map[string]any{
		"ts":      l.now().UTC().Format(time.RFC3339Nano),
		"level":   strings.ToLower(string(level)),
		"message": SanitizeString(message),
	}

	for key, value := range fields {
		safeKey := sanitizeKey(key)
		if safeKey == "" {
			continue
		}
		entry[safeKey] = sanitizeFieldValue(safeKey, value)
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"ts":%q,"level":"error","message":"structured log marshal failure","status":"logger_error"}`, l.now().UTC().Format(time.RFC3339Nano)))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(payload, '\n'))
}

func SanitizeString(input string) string {
	safe := strings.TrimSpace(input)
	if safe == "" {
		return safe
	}

	safe = bearerPattern.ReplaceAllString(safe, "$1 "+redactedValue)
	safe = jwtPattern.ReplaceAllString(safe, redactedValue)
	safe = secretAssignment.ReplaceAllString(safe, "$1="+redactedValue)
	safe = emailPattern.ReplaceAllString(safe, redactedValue)
	return safe
}

func sanitizeKey(key string) string {
	return strings.TrimSpace(strings.ToLower(key))
}

func sanitizeFieldValue(key string, value any) any {
	if isSensitiveField(key) {
		return redactedValue
	}

	switch v := value.(type) {
	case nil:
		return nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return v
	case string:
		return SanitizeString(v)
	case []byte:
		return redactedValue
	case error:
		return SanitizeString(v.Error())
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case fmt.Stringer:
		return SanitizeString(v.String())
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, SanitizeString(item))
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for nestedKey, nestedValue := range v {
			out[sanitizeKey(nestedKey)] = fmt.Sprint(sanitizeFieldValue(nestedKey, nestedValue))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for nestedKey, nestedValue := range v {
			out[sanitizeKey(nestedKey)] = sanitizeFieldValue(nestedKey, nestedValue)
		}
		return out
	default:
		return SanitizeString(fmt.Sprint(v))
	}
}

func isSensitiveField(key string) bool {
	normalized := sanitizeKey(key)
	for _, token := range sensitiveFieldTokens {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

type Decision struct {
	Allow      bool
	Suppressed int
}

type Throttler struct {
	mu     sync.Mutex
	now    func() time.Time
	window time.Duration
	state  map[string]throttleState
}

type throttleState struct {
	windowStart time.Time
	suppressed  int
}

func NewThrottler(window time.Duration) *Throttler {
	if window <= 0 {
		window = 30 * time.Second
	}
	return &Throttler{
		now:    time.Now,
		window: window,
		state:  make(map[string]throttleState),
	}
}

func (t *Throttler) Decide(key string) Decision {
	if t == nil {
		return Decision{Allow: true}
	}

	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	current, ok := t.state[key]
	if !ok || now.Sub(current.windowStart) >= t.window {
		decision := Decision{Allow: true}
		if ok {
			decision.Suppressed = current.suppressed
		}
		t.state[key] = throttleState{windowStart: now}
		return decision
	}

	current.suppressed++
	t.state[key] = current
	return Decision{Allow: false}
}
