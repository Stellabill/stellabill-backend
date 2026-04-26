package worker

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"stellarbill-backend/internal/security"
)

var tracer = otel.Tracer("worker/executor")

// BillingExecutor implements JobExecutor for billing operations
type BillingExecutor struct {
	// Add dependencies like payment gateway, notification service, etc.
}

// NewBillingExecutor creates a new billing job executor
func NewBillingExecutor() *BillingExecutor {
	return &BillingExecutor{}
}

// Execute processes a billing job based on its type
func (e *BillingExecutor) Execute(ctx context.Context, job *Job) error {
	security.ProductionLogger().Info("Executing billing job",
		zap.String("job_id", job.ID),
		zap.String("type", job.Type),
		zap.String("subscription_id", job.SubscriptionID))

	ctx, span := tracer.Start(ctx, "BillingExecutor.Execute",
		trace.WithAttributes(
			attribute.String("job_id", job.ID),
			attribute.String("job_type", job.Type),
			attribute.String("subscription_id", job.SubscriptionID)))
	defer span.End()

	switch job.Type {
	case "charge":
		return e.executeCharge(ctx, job)
	case "invoice":
		return e.executeInvoice(ctx, job)
	case "reminder":
		return e.executeReminder(ctx, job)
	default:
		span.SetStatus(codes.Error, "unknown job type")
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

func (e *BillingExecutor) executeCharge(ctx context.Context, job *Job) error {
	ctx, span := tracer.Start(ctx, "executeCharge",
		trace.WithAttributes(attribute.String("subscription_id", job.SubscriptionID)))
	defer span.End()

	security.ProductionLogger().Info("Processing charge",
		zap.String("subscription_id", job.SubscriptionID))

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		span.SetAttributes(attribute.String("result", "success"))
	}

	return nil
}

func (e *BillingExecutor) executeInvoice(ctx context.Context, job *Job) error {
	ctx, span := tracer.Start(ctx, "executeInvoice",
		trace.WithAttributes(attribute.String("subscription_id", job.SubscriptionID)))
	defer span.End()

	security.ProductionLogger().Info("Generating invoice",
		zap.String("subscription_id", job.SubscriptionID))

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		span.SetAttributes(attribute.String("result", "success"))
	}

	return nil
}

func (e *BillingExecutor) executeReminder(ctx context.Context, job *Job) error {
	ctx, span := tracer.Start(ctx, "executeReminder",
		trace.WithAttributes(attribute.String("subscription_id", job.SubscriptionID)))
	defer span.End()

	security.ProductionLogger().Info("Sending reminder",
		zap.String("subscription_id", job.SubscriptionID))

	select {
	case <-ctx.Done():
		span.RecordError(ctx.Err())
		span.SetStatus(codes.Error, "context cancelled")
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		span.SetAttributes(attribute.String("result", "success"))
	}

	return nil
}

