package outbox

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"stellarbill-backend/internal/db"
)

// SubscriberKeyStatus represents the lifecycle state of a subscriber key.
type SubscriberKeyStatus string

const (
	SubscriberKeyActive  SubscriberKeyStatus = "active"
	SubscriberKeyRevoked SubscriberKeyStatus = "revoked"
	SubscriberKeyExpired SubscriberKeyStatus = "expired"
)

// SubscriberKey stores a subscriber's public JWK used for outbox encryption.
type SubscriberKey struct {
	ID           uuid.UUID           `json:"id"`
	SubscriberID string              `json:"subscriber_id"`
	KeyID        string              `json:"key_id"`
	JWK          json.RawMessage     `json:"jwk"`
	Status       SubscriberKeyStatus   `json:"status"`
	ExpiresAt    *time.Time          `json:"expires_at,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// SubscriberKeyRepository manages subscriber encryption keys.
type SubscriberKeyRepository interface {
	Create(key *SubscriberKey) error
	GetByID(id uuid.UUID) (*SubscriberKey, error)
	ListBySubscriber(subscriberID string) ([]*SubscriberKey, error)
	GetActiveKey(subscriberID string) (*SubscriberKey, error)
	UpdateStatus(id uuid.UUID, status SubscriberKeyStatus) error
}

type postgresSubscriberKeyRepository struct {
	db db.DBTX
}

// NewPostgresSubscriberKeyRepository creates a Postgres-backed key repository.
func NewPostgresSubscriberKeyRepository(executor db.DBTX) SubscriberKeyRepository {
	return &postgresSubscriberKeyRepository{db: executor}
}

func (r *postgresSubscriberKeyRepository) Create(key *SubscriberKey) error {
	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	now := time.Now()
	if key.CreatedAt.IsZero() {
		key.CreatedAt = now
	}
	key.UpdatedAt = now
	if key.Status == "" {
		key.Status = SubscriberKeyActive
	}

	query := `
		INSERT INTO subscriber_keys (
			id, subscriber_id, key_id, jwk, status, expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.Exec(query,
		key.ID, key.SubscriberID, key.KeyID, key.JWK, key.Status, key.ExpiresAt,
		key.CreatedAt, key.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create subscriber key: %w", err)
	}
	return nil
}

func (r *postgresSubscriberKeyRepository) GetByID(id uuid.UUID) (*SubscriberKey, error) {
	query := `
		SELECT id, subscriber_id, key_id, jwk, status, expires_at, created_at, updated_at
		FROM subscriber_keys WHERE id = $1`
	return r.scanKey(r.db.QueryRow(query, id))
}

func (r *postgresSubscriberKeyRepository) ListBySubscriber(subscriberID string) ([]*SubscriberKey, error) {
	query := `
		SELECT id, subscriber_id, key_id, jwk, status, expires_at, created_at, updated_at
		FROM subscriber_keys
		WHERE subscriber_id = $1
		ORDER BY created_at DESC`

	rows, err := r.db.Query(query, subscriberID)
	if err != nil {
		return nil, fmt.Errorf("list subscriber keys: %w", err)
	}
	defer rows.Close()

	var keys []*SubscriberKey
	for rows.Next() {
		key, err := r.scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriber keys: %w", err)
	}
	return keys, nil
}

func (r *postgresSubscriberKeyRepository) GetActiveKey(subscriberID string) (*SubscriberKey, error) {
	query := `
		SELECT id, subscriber_id, key_id, jwk, status, expires_at, created_at, updated_at
		FROM subscriber_keys
		WHERE subscriber_id = $1
		  AND status = $2
		  AND (expires_at IS NULL OR expires_at > $3)
		ORDER BY created_at DESC
		LIMIT 1`

	key, err := r.scanKey(r.db.QueryRow(query, subscriberID, SubscriberKeyActive, time.Now()))
	if err == sql.ErrNoRows {
		return nil, ErrMissingSubscriberKey
	}
	if err != nil {
		return nil, fmt.Errorf("get active subscriber key: %w", err)
	}
	return key, nil
}

func (r *postgresSubscriberKeyRepository) UpdateStatus(id uuid.UUID, status SubscriberKeyStatus) error {
	query := `UPDATE subscriber_keys SET status = $1, updated_at = $2 WHERE id = $3`
	result, err := r.db.Exec(query, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update subscriber key status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *postgresSubscriberKeyRepository) scanKey(scanner interface {
	Scan(dest ...interface{}) error
}) (*SubscriberKey, error) {
	var key SubscriberKey
	var expiresAt sql.NullTime
	err := scanner.Scan(
		&key.ID, &key.SubscriberID, &key.KeyID, &key.JWK, &key.Status,
		&expiresAt, &key.CreatedAt, &key.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	return &key, nil
}
