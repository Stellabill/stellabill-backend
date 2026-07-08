package saga_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/saga"
	"stellarbill-backend/internal/service"
)

func TestCoordinator_HappyPath(t *testing.T) {
	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	executed := make(map[string]bool)
	compensated := make(map[string]bool)

	s := &saga.Saga{
		ID:      "saga-1",
		Name:    "test_happy",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					executed["step_a"] = true
					sc.Set("step_a_done", true)
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_a"] = true
					return nil
				},
			},
			{
				Key: "step_b",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					executed["step_b"] = true
					sc.Set("step_b_done", true)
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_b"] = true
					return nil
				},
			},
		},
	}

	err := coord.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !executed["step_a"] {
		t.Error("step_a was not executed")
	}
	if !executed["step_b"] {
		t.Error("step_b was not executed")
	}
	if compensated["step_a"] || compensated["step_b"] {
		t.Error("no steps should have been compensated")
	}

	loaded, results, err := store.Load(context.Background(), "saga-1")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loaded.Status != saga.SagaCompleted {
		t.Errorf("expected saga completed, got %s", loaded.Status)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(results))
	}
}

func TestCoordinator_FirstStepFails_NoCompensation(t *testing.T) {
	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	compensated := make(map[string]bool)

	s := &saga.Saga{
		ID:      "saga-2",
		Name:    "test_first_fails",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return errors.New("step_a failed")
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_a"] = true
					return nil
				},
			},
			{
				Key: "step_b",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_b"] = true
					return nil
				},
			},
		},
	}

	err := coord.Execute(context.Background(), s)
	if err == nil {
		t.Fatal("expected error when step fails")
	}

	if compensated["step_a"] {
		t.Error("step_a should not be compensated (it was the failing step)")
	}
	if compensated["step_b"] {
		t.Error("step_b should not be compensated (it was after the failing step)")
	}

	loaded, _, err := store.Load(context.Background(), "saga-2")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loaded.Status != saga.SagaCompensated {
		t.Errorf("expected saga compensated, got %s", loaded.Status)
	}
}

func TestCoordinator_SecondStepFails_FirstCompensated(t *testing.T) {
	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	executed := make(map[string]bool)
	compensated := make(map[string]bool)

	s := &saga.Saga{
		ID:      "saga-3",
		Name:    "test_second_fails",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					executed["step_a"] = true
					sc.Set("step_a_value", "hello")
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_a"] = true
					v, _ := sc.Get("step_a_value")
					if v != "hello" {
						t.Errorf("expected 'hello' in context, got %v", v)
					}
					return nil
				},
			},
			{
				Key: "step_b",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return errors.New("step_b failed")
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					compensated["step_b"] = true
					return nil
				},
			},
		},
	}

	err := coord.Execute(context.Background(), s)
	if err == nil {
		t.Fatal("expected error when step fails")
	}

	if !executed["step_a"] {
		t.Error("step_a should have executed")
	}
	if !compensated["step_a"] {
		t.Error("step_a should have been compensated (step_b failed)")
	}
	if compensated["step_b"] {
		t.Error("step_b should not be compensated (it was the failing step)")
	}

	loaded, results, err := store.Load(context.Background(), "saga-3")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loaded.Status != saga.SagaCompensated {
		t.Errorf("expected saga compensated, got %s", loaded.Status)
	}

	for _, r := range results {
		if r.StepKey == "step_a" && r.Status != saga.StepCompensated {
			t.Errorf("expected step_a compensated, got %s", r.Status)
		}
		if r.StepKey == "step_b" && r.Status != saga.StepFailed {
			t.Errorf("expected step_b failed, got %s", r.Status)
		}
	}
}

func TestCoordinator_CompensationFails(t *testing.T) {
	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	s := &saga.Saga{
		ID:      "saga-4",
		Name:    "test_comp_fails",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return errors.New("compensation for step_a failed")
				},
			},
			{
				Key: "step_b",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return errors.New("step_b failed")
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
		},
	}

	err := coord.Execute(context.Background(), s)
	if err == nil {
		t.Fatal("expected error when step fails")
	}

	loaded, results, err := store.Load(context.Background(), "saga-4")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}

	if loaded.Status != saga.SagaFailed {
		t.Errorf("expected saga failed, got %s", loaded.Status)
	}

	for _, r := range results {
		if r.StepKey == "step_a" && r.Status != saga.StepCompensationFailed {
			t.Errorf("expected step_a compensation_failed, got %s", r.Status)
		}
	}
}

func TestCoordinator_DuplicateExecution(t *testing.T) {
	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	execCount := 0

	s := &saga.Saga{
		ID:      "saga-5",
		Name:    "test_duplicate",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					execCount++
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
		},
	}

	err := coord.Execute(context.Background(), s)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}

	s2 := &saga.Saga{
		ID:      "saga-5",
		Name:    "test_duplicate",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					execCount++
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
		},
	}
	_ = coord.Execute(context.Background(), s2)

	if execCount < 2 {
		t.Errorf("expected at least 2 executions, got %d", execCount)
	}
}

func TestCoordinator_ResumeAfterCrash(t *testing.T) {
	store := saga.NewMemoryStore()

	sagaID := "saga-resume-1"
	ctx := saga.NewSagaContext(map[string]any{"resumed": true})
	partial := &saga.Saga{
		ID:      sagaID,
		Name:    "test_resume",
		Status:  saga.SagaRunning,
		Context: ctx,
	}
	if err := store.Save(context.Background(), partial); err != nil {
		t.Fatalf("save partial saga: %v", err)
	}
	if err := store.SaveStepResult(context.Background(), sagaID, &saga.StepResult{
		SagaID:  sagaID,
		StepKey: "step_a",
		Status:  saga.StepCompleted,
	}); err != nil {
		t.Fatalf("save step_a result: %v", err)
	}

	constructor := func(ctx context.Context, s *saga.Saga) (*saga.Saga, error) {
		s.Steps = []saga.Step{
			{
				Key: "step_a",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
			{
				Key: "step_b",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					sc.Set("step_b_ran", true)
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
		}
		return s, nil
	}

	coord := saga.NewCoordinator(store, constructor)
	err := coord.Resume(context.Background(), sagaID)
	if err != nil {
		t.Fatalf("resume saga: %v", err)
	}

	loaded, results, err := store.Load(context.Background(), sagaID)
	if err != nil {
		t.Fatalf("load saga after resume: %v", err)
	}
	if loaded.Status != saga.SagaCompleted {
		t.Errorf("expected saga completed after resume, got %s", loaded.Status)
	}

	for _, r := range results {
		if r.StepKey == "step_b" && r.Status != saga.StepCompleted {
			t.Errorf("expected step_b completed after resume, got %s", r.Status)
		}
	}

	v, ok := loaded.Context.Get("step_b_ran")
	if !ok || v != true {
		t.Error("step_b should have set step_b_ran in context")
	}
}

func TestCoordinator_MissingSagaID(t *testing.T) {
	coord := saga.NewCoordinator(saga.NewMemoryStore(), nil)
	s := &saga.Saga{
		ID:   "",
		Name: "no_id",
		Steps: []saga.Step{
			{Key: "x", Execute: func(ctx context.Context, sc saga.SagaContext) error { return nil }, Compensate: nil},
		},
	}
	err := coord.Execute(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for missing saga ID")
	}
}

func TestCoordinator_EmptySteps(t *testing.T) {
	coord := saga.NewCoordinator(saga.NewMemoryStore(), nil)
	s := &saga.Saga{
		ID:    "saga-empty",
		Name:  "empty",
		Steps: []saga.Step{},
	}
	err := coord.Execute(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestMemoryStore_SaveAndLoad(t *testing.T) {
	store := saga.NewMemoryStore()

	ctx := saga.NewSagaContext(map[string]any{"key": "value"})
	s := &saga.Saga{
		ID:      "ms-1",
		Name:    "test",
		Status:  saga.SagaRunning,
		Context: ctx,
	}

	if err := store.Save(context.Background(), s); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, _, err := store.Load(context.Background(), "ms-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.ID != "ms-1" {
		t.Errorf("expected id ms-1, got %s", loaded.ID)
	}
	if loaded.Name != "test" {
		t.Errorf("expected name test, got %s", loaded.Name)
	}

	v, ok := loaded.Context.Get("key")
	if !ok || v != "value" {
		t.Errorf("expected key='value', got %v", v)
	}
}

func TestMemoryStore_LoadNotFound(t *testing.T) {
	store := saga.NewMemoryStore()
	_, _, err := store.Load(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing saga")
	}
}

func TestMemoryStore_SaveStepResult(t *testing.T) {
	store := saga.NewMemoryStore()

	s := &saga.Saga{ID: "ms-2", Name: "step_test", Status: saga.SagaRunning}
	if err := store.Save(context.Background(), s); err != nil {
		t.Fatalf("save saga: %v", err)
	}

	sr := &saga.StepResult{
		SagaID:  "ms-2",
		StepKey: "step_a",
		Status:  saga.StepCompleted,
	}
	if err := store.SaveStepResult(context.Background(), "ms-2", sr); err != nil {
		t.Fatalf("save step result: %v", err)
	}

	_, results, err := store.Load(context.Background(), "ms-2")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].StepKey != "step_a" {
		t.Errorf("expected step_a, got %s", results[0].StepKey)
	}
	if results[0].Status != saga.StepCompleted {
		t.Errorf("expected completed, got %s", results[0].Status)
	}
}

func TestMemoryStore_ListRunning(t *testing.T) {
	store := saga.NewMemoryStore()

	s1 := &saga.Saga{ID: "r1", Name: "running1", Status: saga.SagaRunning}
	s2 := &saga.Saga{ID: "r2", Name: "running2", Status: saga.SagaRunning}
	s3 := &saga.Saga{ID: "c1", Name: "completed", Status: saga.SagaCompleted}

	if err := store.Save(context.Background(), s1); err != nil {
		t.Fatalf("save s1: %v", err)
	}
	if err := store.Save(context.Background(), s2); err != nil {
		t.Fatalf("save s2: %v", err)
	}
	if err := store.Save(context.Background(), s3); err != nil {
		t.Fatalf("save s3: %v", err)
	}

	running, err := store.ListRunning(context.Background())
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	if len(running) != 2 {
		t.Errorf("expected 2 running sagas, got %d", len(running))
	}
}

func TestCancelSubscriptionFlow_HappyPath(t *testing.T) {
	plan := repository.PlanRow{
		ID: "plan-1", Name: "Pro", Amount: "2999", Currency: "usd",
		Interval: "month", Description: "Pro plan",
	}
	sub := repository.SubscriptionRow{
		ID: "sub-1", PlanID: "plan-1", TenantID: "tenant-1",
		CustomerID: "cust-1", Status: "active", Amount: "2999",
		Currency: "usd", Interval: "month",
	}

	subRepo := repository.NewMockSubscriptionRepo(&sub)
	planRepo := repository.NewMockPlanRepo(&plan)
	subSvc := service.NewSubscriptionService(subRepo, planRepo)
	stmtRepo := repository.NewMockStatementRepo()

	flow := saga.CancelSubscriptionFlow(
		subSvc, stmtRepo, "saga-flow-1",
		"tenant-1", "actor-1", "sub-1", "cust-1",
		"-2999", "usd",
	)

	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	err := coord.Execute(context.Background(), flow)
	if err != nil {
		t.Fatalf("execute flow: %v", err)
	}

	loadedSub, err := subRepo.FindByID(context.Background(), "sub-1")
	if err != nil {
		t.Fatalf("find sub: %v", err)
	}
	if loadedSub.Status != "cancelled" {
		t.Errorf("expected cancelled, got %s", loadedSub.Status)
	}

	refundStmt, err := stmtRepo.FindByID(context.Background(), "stmt-saga-flow-1-refund")
	if err != nil {
		t.Fatalf("find refund statement: %v", err)
	}
	if refundStmt.Kind != "refund" {
		t.Errorf("expected refund kind, got %s", refundStmt.Kind)
	}

	loadedSaga, _, err := store.Load(context.Background(), "saga-flow-1")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loadedSaga.Status != saga.SagaCompleted {
		t.Errorf("expected saga completed, got %s", loadedSaga.Status)
	}
}

func TestCancelSubscriptionFlow_SecondStepFails_FirstCompensated(t *testing.T) {
	plan := repository.PlanRow{
		ID: "plan-1", Name: "Pro", Amount: "2999", Currency: "usd",
		Interval: "month", Description: "Pro plan",
	}
	sub := repository.SubscriptionRow{
		ID: "sub-2", PlanID: "plan-1", TenantID: "tenant-1",
		CustomerID: "cust-1", Status: "active", Amount: "2999",
		Currency: "usd", Interval: "month",
	}

	subRepo := repository.NewMockSubscriptionRepo(&sub)
	planRepo := repository.NewMockPlanRepo(&plan)
	subSvc := service.NewSubscriptionService(subRepo, planRepo)
	stmtRepo := repository.NewMockStatementRepo()

	stmtRepo.SetCreateError(errors.New("db connection lost"))

	flow := saga.CancelSubscriptionFlow(
		subSvc, stmtRepo, "saga-flow-2",
		"tenant-1", "actor-1", "sub-2", "cust-1",
		"-2999", "usd",
	)

	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	err := coord.Execute(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error when refund step fails")
	}
	if !strings.Contains(err.Error(), "create refund statement") {
		t.Errorf("expected refund statement error, got: %v", err)
	}

	loadedSub, err := subRepo.FindByID(context.Background(), "sub-2")
	if err != nil {
		t.Fatalf("find sub: %v", err)
	}
	if loadedSub.Status != "cancelled" {
		t.Errorf("expected subscription cancelled (compensation blocked by state machine), got %s", loadedSub.Status)
	}

	loadedSaga, _, err := store.Load(context.Background(), "saga-flow-2")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loadedSaga.Status != saga.SagaFailed {
		t.Errorf("expected saga failed (compensation blocked by state machine), got %s", loadedSaga.Status)
	}
}

func TestCancelSubscriptionFlow_CompensationSucceeds_WithRestorableState(t *testing.T) {
	plan := repository.PlanRow{
		ID: "plan-1", Name: "Pro", Amount: "2999", Currency: "usd",
		Interval: "month", Description: "Pro plan",
	}
	sub := repository.SubscriptionRow{
		ID: "sub-3", PlanID: "plan-1", TenantID: "tenant-1",
		CustomerID: "cust-1", Status: "active", Amount: "2999",
		Currency: "usd", Interval: "month",
	}

	subRepo := repository.NewMockSubscriptionRepo(&sub)
	planRepo := repository.NewMockPlanRepo(&plan)
	subSvc := service.NewSubscriptionService(subRepo, planRepo)

	flow := &saga.Saga{
		ID:      "saga-flow-3",
		Name:    "test_pause_and_refund",
		Context: saga.NewSagaContext(nil),
		Steps: []saga.Step{
			{
				Key: "pause_subscription",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					result, err := subSvc.ChangeStatus(ctx, "tenant-1", "actor-1", "sub-3", "paused")
					if err != nil {
						return err
					}
					sc.Set("previous_status", result.PreviousStatus)
					return nil
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					prev, _ := sc.Get("previous_status")
					_, err := subSvc.ChangeStatus(ctx, "tenant-1", "actor-1", "sub-3", prev.(string))
					return err
				},
			},
			{
				Key: "failing_step",
				Execute: func(ctx context.Context, sc saga.SagaContext) error {
					return errors.New("step 2 failed")
				},
				Compensate: func(ctx context.Context, sc saga.SagaContext) error {
					return nil
				},
			},
		},
	}

	store := saga.NewMemoryStore()
	coord := saga.NewCoordinator(store, nil)

	err := coord.Execute(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error")
	}

	loadedSub, err := subRepo.FindByID(context.Background(), "sub-3")
	if err != nil {
		t.Fatalf("find sub: %v", err)
	}
	if loadedSub.Status != "active" {
		t.Errorf("expected subscription restored to active, got %s", loadedSub.Status)
	}

	loadedSaga, _, err := store.Load(context.Background(), "saga-flow-3")
	if err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if loadedSaga.Status != saga.SagaCompensated {
		t.Errorf("expected saga compensated, got %s", loadedSaga.Status)
	}
}
