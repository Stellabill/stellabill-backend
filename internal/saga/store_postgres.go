package saga

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"stellarbill-backend/internal/db"
)

type postgresStore struct {
	dbtx db.DBTX
}

func NewPostgresStore(dbtx db.DBTX) Store {
	return &postgresStore{dbtx: dbtx}
}

func (s *postgresStore) Save(ctx context.Context, saga *Saga) error {
	contextJSON, err := json.Marshal(saga.Context)
	if err != nil {
		return fmt.Errorf("marshal saga context: %w", err)
	}

	const query = `
		INSERT INTO saga_instances (id, name, status, context, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			context = EXCLUDED.context,
			updated_at = EXCLUDED.updated_at`

	_, err = s.dbtx.ExecContext(ctx, query,
		saga.ID,
		saga.Name,
		string(saga.Status),
		contextJSON,
		saga.CreatedAt,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("save saga instance: %w", err)
	}

	return nil
}

func (s *postgresStore) SaveStepResult(ctx context.Context, sagaID string, sr *StepResult) error {
	const query = `
		INSERT INTO saga_step_results (saga_id, step_key, status, error_message, executed_at, compensated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (saga_id, step_key) DO UPDATE SET
			status = EXCLUDED.status,
			error_message = EXCLUDED.error_message,
			executed_at = COALESCE(EXCLUDED.executed_at, saga_step_results.executed_at),
			compensated_at = COALESCE(EXCLUDED.compensated_at, saga_step_results.compensated_at)`

	_, err := s.dbtx.ExecContext(ctx, query,
		sagaID,
		sr.StepKey,
		string(sr.Status),
		nilString(sr.ErrorMessage),
		nilTime(sr.ExecutedAt),
		nilTime(sr.CompensatedAt),
	)
	if err != nil {
		return fmt.Errorf("save step result: %w", err)
	}

	return nil
}

func (s *postgresStore) Load(ctx context.Context, sagaID string) (*Saga, []StepResult, error) {
	const sagaQuery = `
		SELECT id, name, status, context, created_at, updated_at
		FROM saga_instances
		WHERE id = $1`

	row := s.dbtx.QueryRowContext(ctx, sagaQuery, sagaID)

	var saga Saga
	var contextJSON []byte
	var statusStr string

	err := row.Scan(&saga.ID, &saga.Name, &statusStr, &contextJSON, &saga.CreatedAt, &saga.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("saga %s: %w", sagaID, ErrNotFound)
		}
		return nil, nil, fmt.Errorf("scan saga instance: %w", err)
	}

	saga.Status = SagaStatus(statusStr)

	if len(contextJSON) > 0 {
		if err := json.Unmarshal(contextJSON, &saga.Context); err != nil {
			return nil, nil, fmt.Errorf("unmarshal saga context: %w", err)
		}
	}

	const stepsQuery = `
		SELECT saga_id, step_key, status, error_message, executed_at, compensated_at
		FROM saga_step_results
		WHERE saga_id = $1
		ORDER BY step_key`

	stepRows, err := s.dbtx.QueryContext(ctx, stepsQuery, sagaID)
	if err != nil {
		return nil, nil, fmt.Errorf("query step results: %w", err)
	}
	defer stepRows.Close()

	var results []StepResult
	for stepRows.Next() {
		var sr StepResult
		var statusStr, errMsg sql.NullString
		var execAt, compAt sql.NullTime

		if err := stepRows.Scan(&sr.SagaID, &sr.StepKey, &statusStr, &errMsg, &execAt, &compAt); err != nil {
			return nil, nil, fmt.Errorf("scan step result: %w", err)
		}

		sr.Status = StepStatus(statusStr.String)
		if errMsg.Valid {
			sr.ErrorMessage = errMsg.String
		}
		if execAt.Valid {
			sr.ExecutedAt = &execAt.Time
		}
		if compAt.Valid {
			sr.CompensatedAt = &compAt.Time
		}

		results = append(results, sr)
	}

	if err := stepRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate step results: %w", err)
	}

	return &saga, results, nil
}

func (s *postgresStore) ListRunning(ctx context.Context) ([]*Saga, error) {
	const query = `
		SELECT id, name, status, context, created_at, updated_at
		FROM saga_instances
		WHERE status IN ('running', 'compensating')
		ORDER BY created_at`

	rows, err := s.dbtx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list running sagas: %w", err)
	}
	defer rows.Close()

	var sagas []*Saga
	for rows.Next() {
		var saga Saga
		var contextJSON []byte
		var statusStr string

		if err := rows.Scan(&saga.ID, &saga.Name, &statusStr, &contextJSON, &saga.CreatedAt, &saga.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan saga instance: %w", err)
		}

		saga.Status = SagaStatus(statusStr)

		if len(contextJSON) > 0 {
			if err := json.Unmarshal(contextJSON, &saga.Context); err != nil {
				return nil, fmt.Errorf("unmarshal saga context: %w", err)
			}
		}

		sagas = append(sagas, &saga)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate running sagas: %w", err)
	}

	return sagas, nil
}

func nilString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilTime(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}
