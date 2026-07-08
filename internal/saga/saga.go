package saga

import (
	"context"
	"encoding/json"
	"time"
)

type StepStatus string

const (
	StepPending           StepStatus = "pending"
	StepRunning           StepStatus = "running"
	StepCompleted         StepStatus = "completed"
	StepFailed            StepStatus = "failed"
	StepCompensating      StepStatus = "compensating"
	StepCompensated       StepStatus = "compensated"
	StepCompensationFailed StepStatus = "compensation_failed"
)

type SagaStatus string

const (
	SagaRunning     SagaStatus = "running"
	SagaCompleted   SagaStatus = "completed"
	SagaFailed      SagaStatus = "failed"
	SagaCompensating SagaStatus = "compensating"
	SagaCompensated  SagaStatus = "compensated"
)

type StepFn func(ctx context.Context, sagaCtx SagaContext) error

type Step struct {
	Key        string
	Execute    StepFn
	Compensate StepFn
}

type StepResult struct {
	SagaID        string     `json:"saga_id"`
	StepKey       string     `json:"step_key"`
	Status        StepStatus `json:"status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	ExecutedAt    *time.Time `json:"executed_at,omitempty"`
	CompensatedAt *time.Time `json:"compensated_at,omitempty"`
}

type SagaContext struct {
	raw map[string]any
}

func NewSagaContext(initial map[string]any) SagaContext {
	if initial == nil {
		initial = make(map[string]any)
	}
	return SagaContext{raw: initial}
}

func (sc SagaContext) Set(key string, value any) {
	sc.raw[key] = value
}

func (sc SagaContext) Get(key string) (any, bool) {
	v, ok := sc.raw[key]
	return v, ok
}

func (sc SagaContext) Raw() map[string]any {
	return sc.raw
}

func (sc SagaContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(sc.raw)
}

func (sc *SagaContext) UnmarshalJSON(data []byte) error {
	if sc.raw == nil {
		sc.raw = make(map[string]any)
	}
	return json.Unmarshal(data, &sc.raw)
}

type Saga struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Status    SagaStatus  `json:"status"`
	Context   SagaContext `json:"context"`
	Steps     []Step      `json:"-"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Coordinator interface {
	Execute(ctx context.Context, saga *Saga) error
	Resume(ctx context.Context, sagaID string) error
}

type Store interface {
	Save(ctx context.Context, saga *Saga) error
	Load(ctx context.Context, sagaID string) (*Saga, []StepResult, error)
	SaveStepResult(ctx context.Context, sagaID string, sr *StepResult) error
	ListRunning(ctx context.Context) ([]*Saga, error)
}

type SagaConstructor func(ctx context.Context, saga *Saga) (*Saga, error)
