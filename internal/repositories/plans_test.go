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

func TestPostgresPlanRepository_ReadRouting(t *testing.T) {
	primaryDB, primaryMock, err := sqlmock.New()
	require.NoError(t, err)
	defer primaryDB.Close()

	replicaDB, replicaMock, err := sqlmock.New()
	require.NoError(t, err)
	defer replicaDB.Close()

	router := db.NewReadRouter(primaryDB, replicaDB)
	repo := NewPlanRepository(router)

	t.Run("GetByID routes to replica when no freshness token in context", func(t *testing.T) {
		ctx := context.Background()

		replicaMock.ExpectQuery("SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \\$1").
			WithArgs("plan-123").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
				AddRow("plan-123", "Basic Plan", "1000", "USD", "month", "Basic description", "merchant-1", time.Now(), time.Now()))

		plan, err := repo.GetByID(ctx, "plan-123")
		require.NoError(t, err)
		assert.Equal(t, "plan-123", plan.ID)
		assert.Equal(t, "Basic Plan", plan.Name)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("GetByID routes to primary when freshness token is present", func(t *testing.T) {
		ctx := db.WithFreshnessToken(context.Background(), "token-123")

		primaryMock.ExpectQuery("SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \\$1").
			WithArgs("plan-123").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
				AddRow("plan-123", "Basic Plan", "1000", "USD", "month", "Basic description", "merchant-1", time.Now(), time.Now()))

		plan, err := repo.GetByID(ctx, "plan-123")
		require.NoError(t, err)
		assert.Equal(t, "plan-123", plan.ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("Create always routes to primary", func(t *testing.T) {
		plan := &Plan{
			ID:         "plan-456",
			Name:       "Pro Plan",
			Amount:     "2000",
			Currency:   "USD",
			Interval:   "month",
			MerchantID: "merchant-1",
		}

		primaryMock.ExpectQuery("INSERT INTO plans").
			WithArgs(plan.ID, plan.Name, plan.Amount, plan.Currency, plan.Interval, plan.Description, plan.MerchantID, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(plan.ID))

		err := repo.Create(plan)
		require.NoError(t, err)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})
}
