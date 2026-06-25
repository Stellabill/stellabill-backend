package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/db"
)

func TestPostgresSubscriptionRepository_ReadRouting(t *testing.T) {
	primaryDB, primaryMock, err := sqlmock.New()
	require.NoError(t, err)
	defer primaryDB.Close()

	replicaDB, replicaMock, err := sqlmock.New()
	require.NoError(t, err)
	defer replicaDB.Close()

	router := db.NewReadRouter(primaryDB, replicaDB)
	repo := NewSubscriptionRepository(router)

	t.Run("GetByID routes to replica when no freshness token in context", func(t *testing.T) {
		ctx := context.Background()

		replicaMock.ExpectQuery("SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,.* FROM subscriptions WHERE id = \\$1").
			WithArgs("sub-123").
			WillReturnRows(sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
				AddRow("sub-123", "plan-1", "cust-1", "merch-1", "active", "1000", "USD", "month", time.Now(), time.Now(), false, nil, nil, nil, nil, time.Now(), time.Now()))

		sub, err := repo.GetByID(ctx, "sub-123")
		require.NoError(t, err)
		assert.Equal(t, "sub-123", sub.ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("GetByID routes to primary when freshness token is present", func(t *testing.T) {
		ctx := db.WithFreshnessToken(context.Background(), "token-123")

		primaryMock.ExpectQuery("SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,.* FROM subscriptions WHERE id = \\$1").
			WithArgs("sub-123").
			WillReturnRows(sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
				AddRow("sub-123", "plan-1", "cust-1", "merch-1", "active", "1000", "USD", "month", time.Now(), time.Now(), false, nil, nil, nil, nil, time.Now(), time.Now()))

		sub, err := repo.GetByID(ctx, "sub-123")
		require.NoError(t, err)
		assert.Equal(t, "sub-123", sub.ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("UpdateStatus always routes to primary", func(t *testing.T) {
		primaryMock.ExpectExec("UPDATE subscriptions SET status = \\$1, updated_at = \\$2 WHERE id = \\$3").
			WithArgs("canceled", sqlmock.AnyArg(), "sub-123").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus("sub-123", "canceled")
		require.NoError(t, err)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})
}
