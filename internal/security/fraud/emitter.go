package fraud

import (
	"context"

	"stellarbill-backend/internal/audit"
)

// auditEmitter adapts the project's audit.Logger to the Emitter interface.
type auditEmitter struct {
	logger *audit.Logger
}

// Adapt wraps an *audit.Logger so it can be used as a fraud Emitter. It returns
// nil when logger is nil, which NewCollector treats as shadow (no-emit) mode.
func Adapt(logger *audit.Logger) Emitter {
	if logger == nil {
		return nil
	}
	return &auditEmitter{logger: logger}
}

// Emit converts a fraud AuditEvent into the canonical audit.AuditEvent and logs
// it. The tenant hash is used as the actor/resource so no raw tenant identifier
// is persisted. Metadata carries only counts and thresholds — never PII.
func (a *auditEmitter) Emit(e AuditEvent) error {
	_, err := a.logger.Log(context.Background(), audit.AuditEvent{
		Timestamp: e.DetectedAt,
		Actor:     e.TenantHash,
		Action:    AuditAction,
		Resource:  string(e.Signal),
		Outcome:   "flagged",
		Metadata: map[string]interface{}{
			"signal":      string(e.Signal),
			"count":       e.Count,
			"threshold":   e.Threshold,
			"window":      e.Window,
			"tenant_hash": e.TenantHash,
		},
	})
	return err
}

// ObserveAuthFailure records a failed authentication attempt for tenant.
func (c *Collector) ObserveAuthFailure(tenant string) bool {
	return c.Observe(tenant, SignalAuthFailRate)
}

// ObserveSubscriptionIDMiss records a subscription-ID lookup that did not
// resolve to a resource owned by tenant (enumeration probing).
func (c *Collector) ObserveSubscriptionIDMiss(tenant string) bool {
	return c.Observe(tenant, SignalSubscriptionIDMisses)
}

// ObservePlanChange records a plan change for tenant (churn tracking).
func (c *Collector) ObservePlanChange(tenant string) bool {
	return c.Observe(tenant, SignalPlanChurnRate)
}
