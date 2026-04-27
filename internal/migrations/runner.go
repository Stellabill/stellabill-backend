package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AppliedMigration struct {
	Version   int64
	Name      string
	AppliedAt time.Time
}

type Runner struct {
	DB *sql.DB
}

func (r Runner) Validate() error {
	if r.DB == nil {
		return errors.New("DB is required")
	}
	return nil
}

func (r Runner) EnsureSchemaMigrations(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version BIGINT PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`)
	return err
}

func (r Runner) lock(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `LOCK TABLE schema_migrations IN EXCLUSIVE MODE;`)
	return err
}

func (r Runner) Applied(ctx context.Context) ([]AppliedMigration, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := r.EnsureSchemaMigrations(ctx, tx); err != nil {
		return nil, err
	}
	if err := r.lock(ctx, tx); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AppliedMigration
	for rows.Next() {
		var m AppliedMigration
		if err := rows.Scan(&m.Version, &m.Name, &m.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return out, nil
}

func (r Runner) Up(ctx context.Context, migs []Migration) ([]Migration, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if len(migs) == 0 {
		return nil, errors.New("no migrations provided")
	}

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := r.EnsureSchemaMigrations(ctx, tx); err != nil {
		return nil, err
	}
	if err := r.lock(ctx, tx); err != nil {
		return nil, err
	}

	appliedSet, err := r.appliedVersions(ctx, tx)
	if err != nil {
		return nil, err
	}

	var appliedNow []Migration
	for _, m := range migs {
		if _, ok := appliedSet[m.Version]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
			return nil, fmt.Errorf("apply up %d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2);`, m.Version, m.Name); err != nil {
			return nil, fmt.Errorf("record migration %d_%s: %w", m.Version, m.Name, err)
		}
		appliedNow = append(appliedNow, m)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return appliedNow, nil
}

func (r Runner) Down(ctx context.Context, migs []Migration) (*Migration, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if len(migs) == 0 {
		return nil, errors.New("no migrations provided")
	}

	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := r.EnsureSchemaMigrations(ctx, tx); err != nil {
		return nil, err
	}
	if err := r.lock(ctx, tx); err != nil {
		return nil, err
	}

	var version int64
	var name string
	err = tx.QueryRowContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1;`).Scan(&version, &name)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	m, ok := FindByVersion(migs, version)
	if !ok {
		return nil, fmt.Errorf("database has applied migration version %d not present locally", version)
	}
	if _, err := tx.ExecContext(ctx, m.DownSQL); err != nil {
		return nil, fmt.Errorf("apply down %d_%s: %w", m.Version, m.Name, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1;`, version); err != nil {
		return nil, fmt.Errorf("remove migration record %d_%s: %w", m.Version, m.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return &m, nil
}

func (r Runner) appliedVersions(ctx context.Context, tx *sql.Tx) (map[int64]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]struct{}{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

