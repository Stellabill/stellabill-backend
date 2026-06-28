package cache

import "context"

// ObjectStore defines the interface for cold storage operations.
// It abstracts over S3, GCS, or other object storage backends.
type ObjectStore interface {
	// Put writes data to the object store at the given key.
	// It returns the full key/path if successful, or an error.
	Put(ctx context.Context, key string, data []byte) (string, error)

	// Get reads data from the object store at the given key.
	// It returns ErrNotFound if the key doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Delete removes the object at the given key.
	// It returns ErrNotFound if the key doesn't exist (idempotent).
	Delete(ctx context.Context, key string) error
}

// ErrNotFound is returned when an object doesn't exist in the store.
type ErrNotFound struct{}

func (e ErrNotFound) Error() string {
	return "object not found"
}

// StatementArchivePayload represents a serialized statement stored in cold storage.
type StatementArchivePayload struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
	PeriodStart    string `json:"period_start"`
	PeriodEnd      string `json:"period_end"`
	IssuedAt       string `json:"issued_at"`
	TotalAmount    string `json:"total_amount"`
	Currency       string `json:"currency"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	ArchivedAt     string `json:"archived_at"`
}
