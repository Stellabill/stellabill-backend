package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"stellarbill-backend/internal/outbox"
)

var webhookInboxLag = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "webhook_inbox_lag_seconds",
	Help:    "Time elapsed between receiving a webhook and successfully processing it",
	Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300},
})

type WebhookEvent struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
}

type WebhookWorker struct {
	DB         *pgxpool.Pool
	OutboxRepo outbox.Repository
	// Additional internal services (e.g., SubscriptionService, StatementService) 
	// can be injected here for direct synchronous database updates if needed.
}

func (w *WebhookWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *WebhookWorker) processBatch(ctx context.Context) {
	query := `
		WITH next_available AS (
			SELECT id
			FROM webhook_inbox w1
			WHERE status = 'pending'
			  AND NOT EXISTS (
				  SELECT 1 FROM webhook_inbox w2
				  WHERE w2.source_id = w1.source_id
					AND w2.status IN ('pending', 'processing')
					AND w2.created_at < w1.created_at
			  )
			ORDER BY created_at ASC
			LIMIT 50
			FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_inbox wi
		SET status = 'processing', attempts = attempts + 1, updated_at = NOW()
		FROM next_available na
		WHERE wi.id = na.id
		RETURNING wi.id, wi.payload, wi.created_at
	`

	rows, err := w.DB.Query(ctx, query)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var payload []byte
		var createdAt time.Time

		if err := rows.Scan(&id, &payload, &createdAt); err != nil {
			continue
		}

		err = w.routePayload(ctx, payload)

		if err != nil {
			w.DB.Exec(ctx, `
				UPDATE webhook_inbox 
				SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END, 
					error_text = $1, 
					updated_at = NOW() 
				WHERE id = $2`,
				err.Error(), id)
		} else {
			w.DB.Exec(ctx, "UPDATE webhook_inbox SET status = 'processed', updated_at = NOW() WHERE id = $1", id)
			webhookInboxLag.Observe(time.Since(createdAt).Seconds())
		}
	}
}

func (w *WebhookWorker) routePayload(ctx context.Context, payload []byte) error {
	var event WebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	switch event.EventType {
	case "subscription.created":
		return w.processSubscriptionCreated(ctx, event)
	case "statement.issued":
		return w.processStatementIssued(ctx, event)
	default:
		return fmt.Errorf("unrecognised event_type: %s", event.EventType)
	}
}

func (w *WebhookWorker) processSubscriptionCreated(ctx context.Context, event WebhookEvent) error {
	subscriptionID, ok := event.Data["subscription_id"].(string)
	if !ok || subscriptionID == "" {
		return fmt.Errorf("missing_field: data.subscription_id is required")
	}

	aggregateType := "subscription"

	outboxEvent, err := outbox.NewEventWithDeduplication(
		"internal.subscription.provisioned",
		event.Data,
		&subscriptionID,
		&aggregateType,
		&subscriptionID,
	)
	if err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	if err := w.OutboxRepo.Store(outboxEvent); err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}

	return nil
}

func (w *WebhookWorker) processStatementIssued(ctx context.Context, event WebhookEvent) error {
	statementID, ok := event.Data["statement_id"].(string)
	if !ok || statementID == "" {
		return fmt.Errorf("missing_field: data.statement_id is required")
	}

	aggregateType := "statement"

	outboxEvent, err := outbox.NewEventWithDeduplication(
		"internal.statement.ready",
		event.Data,
		&statementID,
		&aggregateType,
		&statementID,
	)
	if err != nil {
		return fmt.Errorf("failed to create outbox event: %w", err)
	}

	if err := w.OutboxRepo.Store(outboxEvent); err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}

	return nil
}