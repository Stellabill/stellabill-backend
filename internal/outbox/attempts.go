package outbox

import (
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxResponseBodyBytes = 4096

// Attempt records a single delivery attempt for an outbox event.
type Attempt struct {
	ID            uuid.UUID  `json:"id"`
	EventID       uuid.UUID  `json:"event_id"`
	TenantID      string     `json:"tenant_id"`
	AttemptNumber int        `json:"attempt_number"`
	ResponseCode  *int       `json:"response_code,omitempty"`
	LatencyMs     *int       `json:"latency_ms,omitempty"`
	ResponseBody  *string    `json:"response_body,omitempty"`
	ErrorMessage  *string    `json:"error_message,omitempty"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	AttemptedAt   time.Time  `json:"attempted_at"`
}

// AttemptRepository stores and retrieves delivery attempts.
type AttemptRepository interface {
	// SaveAttempt persists an attempt. Response body is truncated + PII-scrubbed.
	SaveAttempt(a *Attempt) error
	// ListAttempts returns attempts for eventID scoped to tenantID, newest first.
	ListAttempts(tenantID string, eventID uuid.UUID) ([]*Attempt, error)
}

// TruncateAndScrubBody caps body at 4 KB and redacts PII field patterns.
// It is exported so the dispatcher can call it before persisting.
func TruncateAndScrubBody(body string) string {
	if len(body) > maxResponseBodyBytes {
		// Truncate on a valid rune boundary.
		b := []byte(body[:maxResponseBodyBytes])
		for !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
		body = string(b)
	}
	return body
}

// --- In-memory implementation (dev / unit-test) ---

type memAttemptRepository struct {
	mu       sync.RWMutex
	attempts []*Attempt
}

// NewMemAttemptRepository returns a thread-safe in-memory AttemptRepository.
func NewMemAttemptRepository() AttemptRepository {
	return &memAttemptRepository{}
}

func (r *memAttemptRepository) SaveAttempt(a *Attempt) error {
	if a == nil {
		return fmt.Errorf("attempt must not be nil")
	}
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.AttemptedAt.IsZero() {
		a.AttemptedAt = time.Now().UTC()
	}
	// Scrub + truncate response body before storing.
	if a.ResponseBody != nil {
		scrubbed := TruncateAndScrubBody(*a.ResponseBody)
		a.ResponseBody = &scrubbed
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = append(r.attempts, a)
	return nil
}

func (r *memAttemptRepository) ListAttempts(tenantID string, eventID uuid.UUID) ([]*Attempt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*Attempt
	for _, a := range r.attempts {
		if a.EventID == eventID && a.TenantID == tenantID {
			out = append(out, a)
		}
	}
	// Newest first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].AttemptedAt.After(out[j].AttemptedAt)
	})
	return out, nil
}
