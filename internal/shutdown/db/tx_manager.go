package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

type TxManager struct {
	db *sql.DB

	mu       sync.Mutex
	activeTx int
}

// NewTxManager initializes manager
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{
		db: db,
	}
}

// BeginTx creates a new safe transaction
func (tm *TxManager) BeginTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	tm.mu.Lock()
	tm.activeTx++
	tm.mu.Unlock()

	return tx, nil
}

// Done must be called after commit/rollback
func (tm *TxManager) Done() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.activeTx--
}

// ActiveTx returns number of running transactions
func (tm *TxManager) ActiveTx() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.activeTx
}

// Wait blocks until all transactions complete OR timeout
func (tm *TxManager) Wait(ctx context.Context) error {
	for {
		if tm.ActiveTx() == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for transactions to finish")
		default:
			// small sleep to avoid busy loop
			time.Sleep(50 * time.Millisecond)
		}
	}
}
