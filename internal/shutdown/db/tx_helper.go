package db

import (
	"context"
	"database/sql"
)

// WithTransaction safely runs a function inside a transaction
func (tm *TxManager) WithTransaction(ctx context.Context, fn func(*sql.Tx) error) error {

	
	tx, err := tm.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tm.Done()

	// Ensure rollback safety
	defer func() {
		_ = tx.Rollback()
	}()

	err = fn(tx)
	if err != nil {
		return err
	}

	// Check if context was cancelled before commit
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return tx.Commit()
}
