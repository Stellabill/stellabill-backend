package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"stellarbill-backend/internal/integrations/pagerduty"
)

const (
	// dedupKey is stable across restarts so PagerDuty deduplicates the incident.
	deadLetterDedupKey = "stellabill-outbox-dead-letter-spike"
)

// AlertClient is the minimal interface the watcher needs from the PagerDuty client.
type AlertClient interface {
	Trigger(ctx context.Context, dedupKey, summary string, sev pagerduty.Severity, details map[string]any) error
	Resolve(ctx context.Context, dedupKey string) error
}

// DeadLetterWatcherConfig holds watcher settings.
type DeadLetterWatcherConfig struct {
	// Threshold is the minimum number of newly-failed events in one Window
	// that must accumulate before an incident is triggered.
	Threshold int
	// Window is the look-back period used to count inflow.
	Window time.Duration
	// PollInterval controls how often the watcher queries the view.
	PollInterval time.Duration
}

// DefaultDeadLetterWatcherConfig returns safe production defaults.
func DefaultDeadLetterWatcherConfig() DeadLetterWatcherConfig {
	return DeadLetterWatcherConfig{
		Threshold:    5,
		Window:       time.Minute,
		PollInterval: time.Minute,
	}
}

// DeadLetterWatcher polls dead_letter_events and pages via PagerDuty when the
// inflow rate crosses the configured threshold.
type DeadLetterWatcher struct {
	db      *sql.DB
	pd      AlertClient
	cfg     DeadLetterWatcherConfig
	firing  bool // tracks whether an incident is currently open
}

// NewDeadLetterWatcher creates a watcher. db must be connected; pd may be nil
// (in which case alert calls are skipped — useful when PAGERDUTY_ROUTING_KEY
// is unset).
func NewDeadLetterWatcher(db *sql.DB, pd AlertClient, cfg DeadLetterWatcherConfig) *DeadLetterWatcher {
	return &DeadLetterWatcher{db: db, pd: pd, cfg: cfg}
}

// Start runs the watcher loop until ctx is cancelled.
func (w *DeadLetterWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Run once immediately so the first check doesn't wait a full interval.
	w.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Run(ctx)
		}
	}
}

// Run executes a single check cycle. It is exported so tests can drive it
// directly without a ticker.
func (w *DeadLetterWatcher) Run(ctx context.Context) {
	count, err := w.countDeadLetterInflow(ctx)
	if err != nil {
		log.Printf("dead-letter watcher: query error: %v", err)
		return
	}

	above := count >= w.cfg.Threshold

	switch {
	case above && !w.firing:
		w.firing = true
		if w.pd == nil {
			return
		}
		summary := fmt.Sprintf("Outbox dead-letter spike: %d failed events in last %s (threshold %d)",
			count, w.cfg.Window, w.cfg.Threshold)
		err := w.pd.Trigger(ctx, deadLetterDedupKey, summary, pagerduty.SeverityCritical, map[string]any{
			"count":     count,
			"window":    w.cfg.Window.String(),
			"threshold": w.cfg.Threshold,
		})
		if err != nil {
			log.Printf("dead-letter watcher: trigger error: %v", err)
		}

	case !above && w.firing:
		w.firing = false
		if w.pd == nil {
			return
		}
		if err := w.pd.Resolve(ctx, deadLetterDedupKey); err != nil {
			log.Printf("dead-letter watcher: resolve error: %v", err)
		}
	}
}

// countDeadLetterInflow returns the number of events that entered the
// dead_letter_events view (status='failed') within the configured window.
func (w *DeadLetterWatcher) countDeadLetterInflow(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-w.cfg.Window)
	row := w.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM dead_letter_events WHERE updated_at >= $1`, cutoff)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count dead-letter inflow: %w", err)
	}
	return n, nil
}
