package handlers

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateWebhook indicates the (provider, msgID) pair was already stored.
// Callers should treat this as a successful idempotent ack.
var ErrDuplicateWebhook = errors.New("duplicate webhook delivery")

// MemoryWebhookInbox is an in-process InboxRepository for tests and local runs.
type MemoryWebhookInbox struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewMemoryWebhookInbox creates an empty in-memory webhook inbox.
func NewMemoryWebhookInbox() *MemoryWebhookInbox {
	return &MemoryWebhookInbox{seen: make(map[string]struct{})}
}

// Insert records a webhook. Duplicate (provider, msgID) pairs return nil so the
// HTTP layer can still ack with 202.
func (m *MemoryWebhookInbox) Insert(_ context.Context, provider, msgID, sourceID string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := provider + "/" + msgID
	if _, ok := m.seen[key]; ok {
		return nil // deduped — still a successful ack
	}
	m.seen[key] = struct{}{}
	_ = sourceID
	_ = payload
	return nil
}

// Len returns how many unique deliveries have been recorded (test helper).
func (m *MemoryWebhookInbox) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.seen)
}

// SQLWebhookInbox persists verified webhooks into webhook_inbox via database/sql.
type SQLWebhookInbox struct {
	db *sql.DB
}

// NewSQLWebhookInbox wraps a *sql.DB. db must not be nil.
func NewSQLWebhookInbox(db *sql.DB) *SQLWebhookInbox {
	return &SQLWebhookInbox{db: db}
}

// Insert writes a pending inbox row. Unique violations on (provider, provider_msg_id)
// are treated as successful deduplicated deliveries.
func (s *SQLWebhookInbox) Insert(ctx context.Context, provider, msgID, sourceID string, payload []byte) error {
	if s.db == nil {
		return errors.New("webhook inbox database is nil")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_inbox (provider, provider_msg_id, source_id, payload, status)
		VALUES ($1, $2, $3, $4, 'pending')
		ON CONFLICT (provider, provider_msg_id) DO NOTHING`,
		provider, msgID, sourceID, payload,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return err
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return false
}
