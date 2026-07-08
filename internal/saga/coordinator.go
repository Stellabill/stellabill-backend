package saga

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"stellarbill-backend/internal/security"
)

type sagaCoordinator struct {
	store       Store
	constructor SagaConstructor
}

func NewCoordinator(store Store, constructor SagaConstructor) Coordinator {
	return &sagaCoordinator{store: store, constructor: constructor}
}

func (c *sagaCoordinator) Execute(ctx context.Context, saga *Saga) error {
	if saga.ID == "" {
		return errors.New("saga ID is required")
	}
	if len(saga.Steps) == 0 {
		return errors.New("saga must have at least one step")
	}

	saga.Status = SagaRunning
	saga.CreatedAt = time.Now()
	saga.UpdatedAt = time.Now()

	if err := c.store.Save(ctx, saga); err != nil {
		return fmt.Errorf("save saga: %w", err)
	}

	for i, step := range saga.Steps {
		sr := &StepResult{
			SagaID:  saga.ID,
			StepKey: step.Key,
			Status:  StepPending,
		}

		now := time.Now()
		sr.Status = StepRunning
		sr.ExecutedAt = &now
		if err := c.store.SaveStepResult(ctx, saga.ID, sr); err != nil {
			return fmt.Errorf("save step result %s: %w", step.Key, err)
		}

		if err := step.Execute(ctx, saga.Context); err != nil {
			now := time.Now()
			sr.Status = StepFailed
			sr.ExecutedAt = &now
			sr.ErrorMessage = err.Error()
			_ = c.store.SaveStepResult(ctx, saga.ID, sr)

			security.ProductionLogger().Warn("saga step failed, starting compensation",
				zap.String("saga_id", saga.ID),
				zap.String("saga_name", saga.Name),
				zap.String("step_key", step.Key),
				zap.Int("step_index", i),
				zap.Int("total_steps", len(saga.Steps)),
				zap.Error(err),
			)

			c.compensate(ctx, saga, i)
			return fmt.Errorf("saga step %s failed: %w", step.Key, err)
		}

		sr.Status = StepCompleted
		_ = c.store.SaveStepResult(ctx, saga.ID, sr)
	}

	saga.Status = SagaCompleted
	saga.UpdatedAt = time.Now()
	_ = c.store.Save(ctx, saga)

	return nil
}

func (c *sagaCoordinator) compensate(ctx context.Context, saga *Saga, failedIndex int) {
	saga.Status = SagaCompensating
	saga.UpdatedAt = time.Now()
	_ = c.store.Save(ctx, saga)

	allCompensated := true

	for i := failedIndex - 1; i >= 0; i-- {
		step := saga.Steps[i]
		now := time.Now()
		sr := &StepResult{
			SagaID:        saga.ID,
			StepKey:       step.Key,
			Status:        StepCompensating,
			CompensatedAt: &now,
		}
		_ = c.store.SaveStepResult(ctx, saga.ID, sr)

		if err := step.Compensate(ctx, saga.Context); err != nil {
			now := time.Now()
			sr.Status = StepCompensationFailed
			sr.CompensatedAt = &now
			sr.ErrorMessage = err.Error()
			_ = c.store.SaveStepResult(ctx, saga.ID, sr)
			allCompensated = false

			security.ProductionLogger().Error("saga compensation failed",
				zap.String("saga_id", saga.ID),
				zap.String("saga_name", saga.Name),
				zap.String("step_key", step.Key),
				zap.Int("step_index", i),
				zap.Error(err),
			)
			continue
		}

		sr.Status = StepCompensated
		_ = c.store.SaveStepResult(ctx, saga.ID, sr)
	}

	if allCompensated {
		saga.Status = SagaCompensated
	} else {
		saga.Status = SagaFailed
	}
	saga.UpdatedAt = time.Now()
	_ = c.store.Save(ctx, saga)
}

func (c *sagaCoordinator) Resume(ctx context.Context, sagaID string) error {
	saga, results, err := c.store.Load(ctx, sagaID)
	if err != nil {
		return fmt.Errorf("load saga %s: %w", sagaID, err)
	}

	if c.constructor != nil {
		saga, err = c.constructor(ctx, saga)
		if err != nil {
			return fmt.Errorf("reconstruct saga %s: %w", sagaID, err)
		}
	}

	completed := make(map[string]StepStatus)
	for _, r := range results {
		completed[r.StepKey] = r.Status
	}

	switch saga.Status {
	case SagaRunning, SagaCompensating:
	default:
		return nil
	}

	if saga.Status == SagaRunning {
		compensationNeeded := false
		failedIdx := -1

		for i, step := range saga.Steps {
			status, exists := completed[step.Key]

			if !exists || status == StepPending || status == StepRunning {
				sr := &StepResult{SagaID: saga.ID, StepKey: step.Key, Status: StepRunning}
				now := time.Now()
				sr.ExecutedAt = &now
				_ = c.store.SaveStepResult(ctx, saga.ID, sr)

				if err := step.Execute(ctx, saga.Context); err != nil {
					now := time.Now()
					sr.Status = StepFailed
					sr.ExecutedAt = &now
					sr.ErrorMessage = err.Error()
					_ = c.store.SaveStepResult(ctx, saga.ID, sr)
					compensationNeeded = true
					failedIdx = i
					break
				}

				sr.Status = StepCompleted
				_ = c.store.SaveStepResult(ctx, saga.ID, sr)
			} else if status == StepFailed {
				compensationNeeded = true
				failedIdx = i
				break
			}
		}

		if compensationNeeded && failedIdx >= 0 {
			c.compensate(ctx, saga, failedIdx)
		} else if !compensationNeeded {
			saga.Status = SagaCompleted
			saga.UpdatedAt = time.Now()
			_ = c.store.Save(ctx, saga)
		}
	}

	if saga.Status == SagaCompensating {
		for i := len(saga.Steps) - 1; i >= 0; i-- {
			step := saga.Steps[i]
			status, exists := completed[step.Key]
			if !exists || status == StepCompleted {
				now := time.Now()
				sr := &StepResult{
					SagaID:        saga.ID,
					StepKey:       step.Key,
					Status:        StepCompensating,
					CompensatedAt: &now,
				}
				_ = c.store.SaveStepResult(ctx, saga.ID, sr)

				if err := step.Compensate(ctx, saga.Context); err != nil {
					now := time.Now()
					sr.Status = StepCompensationFailed
					sr.CompensatedAt = &now
					sr.ErrorMessage = err.Error()
					_ = c.store.SaveStepResult(ctx, saga.ID, sr)
					continue
				}

				sr.Status = StepCompensated
				_ = c.store.SaveStepResult(ctx, saga.ID, sr)
			}
		}

		allCompensated := true
		for _, r := range results {
			if r.Status == StepCompensationFailed {
				allCompensated = false
				break
			}
		}
		if allCompensated {
			saga.Status = SagaCompensated
		} else {
			saga.Status = SagaFailed
		}
		saga.UpdatedAt = time.Now()
		_ = c.store.Save(ctx, saga)
	}

	return nil
}
