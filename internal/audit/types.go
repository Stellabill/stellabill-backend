package audit

import (
	"time"
)

// Canonical Audit Actions
const (
	ActionAdminLogin    = "admin.login"
	ActionVaultWithdraw = "vault.withdraw"
	ActionConfigUpdate  = "system.config_update"
    // Add other critical actions like "reconciliation.start" or "subscription.mutate" here
)

// Sink defines where audit events are persisted.
type Sink interface {
	WriteEvent(e AuditEvent) error
}

// AuditEvent represents the canonical structure for all security logs.
type AuditEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id"`
	Actor     string                 `json:"actor"`
	Action    string                 `json:"action"`
	Resource  string                 `json:"resource"`
	Outcome   string                 `json:"outcome"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	PrevHash  string                 `json:"prev_hash,omitempty"`
	Hash      string                 `json:"hash,omitempty"`
}
