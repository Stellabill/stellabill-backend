package repositories

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEdgeCases covers comprehensive edge case scenarios for all repositories
func TestEdgeCases_DatabaseConnectionErrors(t *testing.T) {
	t.Run("plan repository with connection closed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		repo := NewPlanRepository(db)

		// Close database connection to simulate connection failure
		db.Close()

		plan := &Plan{
			Name:       "Test Plan",
			Amount:     "29.99",
			Currency:   "USD",
			Interval:   "month",
			MerchantID: "merchant-123",
		}

		err = repo.Create(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create plan")

		// Mock expectations should still be met since we closed the DB
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("subscription repository with connection closed", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		repo := NewSubscriptionRepository(db)

		// Close database connection to simulate connection failure
		db.Close()

		subscription := &Subscription{
			PlanID:             "plan-123",
			CustomerID:         "customer-123",
			MerchantID:         "merchant-123",
			Status:             "active",
			Amount:             "29.99",
			Currency:           "USD",
			Interval:           "month",
			CurrentPeriodStart: time.Now(),
			CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
		}

		err = repo.Create(subscription)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create subscription")

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_DeadlockSimulation(t *testing.T) {
	t.Run("plan update deadlock simulation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewPlanRepository(db)

		plan := &Plan{
			ID:         "plan-123",
			Name:       "Updated Plan",
			Amount:     "39.99",
			Currency:   "USD",
			Interval:   "month",
			MerchantID: "merchant-123",
		}

		// Simulate deadlock error
		mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
			WithArgs("Updated Plan", "39.99", "USD", "month", nil, sqlmock.AnyArg(), "plan-123").
			WillReturnError(fmt.Errorf("deadlock detected"))

		err = repo.Update(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update plan")
		assert.Contains(t, err.Error(), "deadlock detected")

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_NullValueHandling(t *testing.T) {
	t.Run("plan with all null optional fields", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewPlanRepository(db)

		// Test creating plan with null description
		plan := &Plan{
			Name:       "Minimal Plan",
			Amount:     "9.99",
			Currency:   "USD",
			Interval:   "month",
			MerchantID: "merchant-123",
			// Description is intentionally nil
		}

		mock.ExpectQuery(`INSERT INTO plans`).
			WithArgs(sqlmock.AnyArg(), "Minimal Plan", "9.99", "USD", "month", nil, "merchant-123", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("plan-123"))

		err = repo.Create(plan)
		assert.NoError(t, err)

		// Test retrieving plan with null description
		rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
			AddRow(plan.ID, "Minimal Plan", "9.99", "USD", "month", nil, "merchant-123", time.Now(), time.Now())
		mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \$1`).
			WithArgs(plan.ID).
			WillReturnRows(rows)

		retrievedPlan, err := repo.GetByID(plan.ID)
		assert.NoError(t, err)
		assert.NotNil(t, retrievedPlan)
		assert.Nil(t, retrievedPlan.Description)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_ScanErrors(t *testing.T) {
	t.Run("plan scan with invalid data types", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewPlanRepository(db)

		// Create rows with invalid data types
		rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
			AddRow(123, "Invalid Plan", 29.99, 123, 456, time.Now(), 789, "invalid-date", "invalid-date")

		mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE merchant_id = \$1 ORDER BY created_at DESC LIMIT \$2 OFFSET \$3`).
			WithArgs("merchant-123", 10, 0).
			WillReturnRows(rows)

		plans, err := repo.GetByMerchantID("merchant-123", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to scan plan")
		assert.Nil(t, plans)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent plan updates", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewPlanRepository(db)

		plan := &Plan{
			ID:         "plan-123",
			Name:       "Updated Plan",
			Amount:     "39.99",
			Currency:   "USD",
			Interval:   "month",
			MerchantID: "merchant-123",
		}

		// First update succeeds
		mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
			WithArgs("Updated Plan", "39.99", "USD", "month", nil, sqlmock.AnyArg(), "plan-123").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = repo.Update(plan)
		assert.NoError(t, err)

		// Second update fails because plan was deleted by another transaction
		mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
			WithArgs("Updated Plan", "39.99", "USD", "month", nil, sqlmock.AnyArg(), "plan-123").
			WillReturnResult(sqlmock.NewResult(0, 0)) // No rows affected

		err = repo.Update(plan)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan not found")

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_EmptyResults(t *testing.T) {
	t.Run("empty plan results", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewPlanRepository(db)

		// Return empty result set
		rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"})
		mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE merchant_id = \$1 ORDER BY created_at DESC LIMIT \$2 OFFSET \$3`).
			WithArgs("merchant-empty", 10, 0).
			WillReturnRows(rows)

		plans, err := repo.GetByMerchantID("merchant-empty", 10, 0)
		assert.NoError(t, err)
		assert.NotNil(t, plans)
		assert.Len(t, plans, 0)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestEdgeCases_RetryLogic(t *testing.T) {
	t.Run("subscription cancellation with retry simulation", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		repo := NewSubscriptionRepository(db)

		subscriptionID := "sub-123"

		// First attempt fails with connection error
		mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
			WithArgs(false, sqlmock.AnyArg(), sqlmock.AnyArg(), subscriptionID).
			WillReturnError(fmt.Errorf("connection timeout"))

		err = repo.Cancel(subscriptionID, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to cancel subscription")

		// Second attempt succeeds
		mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
			WithArgs(false, sqlmock.AnyArg(), sqlmock.AnyArg(), subscriptionID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = repo.Cancel(subscriptionID, false)
		assert.NoError(t, err)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
