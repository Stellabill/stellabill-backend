package saga

import (
	"context"
	"fmt"
	"time"
)

// Coordinator executes sagas step by step, persisting progress — the saga
// itself plus per-step results — through a Store. It resumes interrupted sagas
// from the first step that has not completed, and compensates already-executed
// steps in reverse order when a later step exhausts its retries.
type coordinator struct {
	store       Store
	constructor SagaConstructor
}

// NewCoordinator creates a coordinator that persists progression to store.
// constructor is used by Resume to rebuild step definitions for a saga loaded
// from the store and may be nil if the caller rebuilds steps itself.
func NewCoordinator(store Store, constructor SagaConstructor) *coordinator {
	return &coordinator{store: store, constructor: constructor}
}

// Execute runs every step of the saga in order. Each step is retried according
// to its RetryPolicy; when a step fails for good, already-executed steps are
// compensated in reverse order. The saga is marked completed, compensated, or
// failed and persisted before this returns.
func (c *coordinator) Execute(ctx context.Context, saga *Saga) error {
	if saga.ID == "" {
		return fmt.Errorf("saga execution requires a saga ID")
	}
	if len(saga.Steps) == 0 {
		return fmt.Errorf("saga %s has no steps to execute", saga.ID)
	}

	if err := c.begin(ctx, saga); err != nil {
		return err
	}

	err := c.runFrom(ctx, saga, nil)
	if err == nil {
		saga.Status = SagaCompleted
		return c.save(ctx, saga)
	}
	return err
}

// Resume reloads a saga and its recorded step results and continues execution
// from the first step that has not completed. Steps already marked completed
// are skipped; steps marked retrying are attempted again.
func (c *coordinator) Resume(ctx context.Context, sagaID string) error {
	saga, results, err := c.store.Load(ctx, sagaID)
	if err != nil {
		return fmt.Errorf("load saga %s for resume: %w", sagaID, err)
	}
	if c.constructor != nil {
		saga, err = c.constructor(ctx, saga)
		if err != nil {
			return fmt.Errorf("reconstruct saga %s: %w", sagaID, err)
		}
	}
	if len(saga.Steps) == 0 {
		return fmt.Errorf("saga %s has no steps to resume", sagaID)
	}

	done := make(map[string]bool, len(results))
	for _, sr := range results {
		done[sr.StepKey] = sr.Status == StepCompleted
	}

	if err := c.begin(ctx, saga); err != nil {
		return err
	}

	err = c.runFrom(ctx, saga, done)
	if err == nil {
		saga.Status = SagaCompleted
		return c.save(ctx, saga)
	}
	return err
}

// runFrom executes every step in saga.Steps, skipping any step marked
// completed in done.
func (c *coordinator) runFrom(ctx context.Context, saga *Saga, done map[string]bool) error {
	for i := range saga.Steps {
		if done[saga.Steps[i].Key] {
			continue
		}
		if err := c.runStep(ctx, saga, i); err != nil {
			c.failAndCompensate(ctx, saga, i)
			return err
		}
	}
	return nil
}

// runStep executes a single step with retries, recording each transition as a
// step result.
func (c *coordinator) runStep(ctx context.Context, saga *Saga, i int) error {
	step := saga.Steps[i]
	now := time.Now().UTC()
	retries := 0
	for {
		err := step.Execute(ctx, saga.Context)
		if err == nil {
			return c.store.SaveStepResult(ctx, saga.ID, &StepResult{
				SagaID:     saga.ID,
				StepKey:    step.Key,
				Status:     StepCompleted,
				ExecutedAt: &now,
			})
		}

		if step.RetryPolicy == nil || !step.RetryPolicy.ShouldRetry(retries+1) {
			_ = c.store.SaveStepResult(ctx, saga.ID, &StepResult{
				SagaID:       saga.ID,
				StepKey:      step.Key,
				Status:       StepFailed,
				ErrorMessage: err.Error(),
				ExecutedAt:   &now,
			})
			return err
		}

		retries++
		SagaStepRetriesTotal.WithLabelValues(saga.Name, step.Key).Inc()
		if rerr := c.store.SaveStepResult(ctx, saga.ID, &StepResult{
			SagaID:       saga.ID,
			StepKey:      step.Key,
			Status:       StepRetrying,
			RetryAttempt: retries,
			ExecutedAt:   &now,
		}); rerr != nil {
			return rerr
		}

		delay := step.RetryPolicy.NextDelay(retries)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// failAndCompensate records the saga as failed or compensated and compensates
// the steps that already succeeded, in reverse order, for the step that just
// exhausted its retries.
func (c *coordinator) failAndCompensate(ctx context.Context, saga *Saga, failedIdx int) {
	compFailed := false
	for j := failedIdx - 1; j >= 0; j-- {
		step := saga.Steps[j]
		compAt := time.Now().UTC()
		if step.Compensate == nil {
			_ = c.store.SaveStepResult(ctx, saga.ID, &StepResult{
				SagaID:        saga.ID,
				StepKey:       step.Key,
				Status:        StepCompensated,
				CompensatedAt: &compAt,
			})
			continue
		}
		if cerr := step.Compensate(ctx, saga.Context); cerr != nil {
			_ = c.store.SaveStepResult(ctx, saga.ID, &StepResult{
				SagaID:        saga.ID,
				StepKey:       step.Key,
				Status:        StepCompensationFailed,
				ErrorMessage:  cerr.Error(),
				CompensatedAt: &compAt,
			})
			compFailed = true
			break
		}
		_ = c.store.SaveStepResult(ctx, saga.ID, &StepResult{
			SagaID:        saga.ID,
			StepKey:       step.Key,
			Status:        StepCompensated,
			CompensatedAt: &compAt,
		})
	}

	if compFailed {
		saga.Status = SagaFailed
	} else {
		saga.Status = SagaCompensated
	}
	_ = c.save(ctx, saga)
}

// begin marks the saga as running and persists its initial state.
func (c *coordinator) begin(ctx context.Context, saga *Saga) error {
	now := time.Now().UTC()
	saga.Status = SagaRunning
	if saga.CreatedAt.IsZero() {
		saga.CreatedAt = now
	}
	saga.UpdatedAt = now
	return c.store.Save(ctx, saga)
}

func (c *coordinator) save(ctx context.Context, saga *Saga) error {
	saga.UpdatedAt = time.Now().UTC()
	return c.store.Save(ctx, saga)
}
