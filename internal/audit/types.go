package audit

import (
	"context"
	"time"
)

// Canonical Audit Actions
const (
	ActionAdminLogin    = "admin.login"
	ActionVaultWithdraw = "vault.withdraw"
	ActionConfigUpdate  = "system.config_update"
    // Add other critical actions like "reconciliation.start" or "subscription.mutate" here
)

// AuditEvent represents the canonical structure for all security logs.
type AuditEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id"`
	Actor     string                 `json:"actor"`
	Action    string                 `json:"action"`
	Resource  string                 `json:"resource"`
	Outcome   string                 `json:"outcome"`
	PrevHash  string                 `json:"prev_hash,omitempty"`
	Hash      string                 `json:"hash,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Sink defines the interface for persisting audit logs.
type Sink interface {
	WriteEvent(event AuditEvent) error
}

type contextKey string

const actorKey contextKey = "audit_actor"

// WithActor injects an actor string into the context.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

// GetActor extracts the actor from the context, defaulting to "anonymous".
func GetActor(ctx context.Context) string {
	if ctx == nil {
		return "anonymous"
	}
	if v := ctx.Value(actorKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "anonymous"
}
