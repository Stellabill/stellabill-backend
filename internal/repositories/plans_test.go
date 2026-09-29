package repositories

import (
	"context"
	"stellarbill-backend/internal/db"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresPlanRepository_ListReadRouting(t *testing.T) {
	primaryDB, primaryMock, err := sqlmock.New()
	require.NoError(t, err)
	defer primaryDB.Close()

	replicaDB, replicaMock, err := sqlmock.New()
	require.NoError(t, err)
	defer replicaDB.Close()

	router := db.NewReadRouter(primaryDB, replicaDB)
	repo := NewPlanRepository(router)

	// GetByMerchantID is the paginated "list plans" endpoint and uses
	// scanPlan (sql.NullString), mirroring the idiom of the GetByID routing test.
	listQuery := "SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE merchant_id = \\$1 ORDER BY created_at DESC LIMIT \\$2 OFFSET \\$3"

	t.Run("GetByMerchantID routes to replica when no freshness token is present", func(t *testing.T) {
		replicaMock.ExpectQuery(listQuery).
			WithArgs("merchant-1", 10, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
				AddRow("plan-1", "Basic", "1000", "USD", "month", "Basic description", "merchant-1", time.Now(), time.Now()))

		plans, err := repo.GetByMerchantID(context.Background(), "merchant-1", 10, 0)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		assert.Equal(t, "plan-1", plans[0].ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("GetByMerchantID routes to primary when freshness token is present (read-your-writes)", func(t *testing.T) {
		ctx := db.WithFreshnessToken(context.Background(), "token-123")

		primaryMock.ExpectQuery(listQuery).
			WithArgs("merchant-1", 10, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
				AddRow("plan-2", "Pro", "2000", "USD", "month", "Pro description", "merchant-1", time.Now(), time.Now()))

		plans, err := repo.GetByMerchantID(ctx, "merchant-1", 10, 0)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		assert.Equal(t, "plan-2", plans[0].ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
		assert.NoError(t, replicaMock.ExpectationsWereMet())
	})

	t.Run("GetByMerchantID falls back to primary when replica is down", func(t *testing.T) {
		downDB, _, err := sqlmock.New()
		require.NoError(t, err)
		downDB.Close() // closing the handle makes PingContext fail → replica marked down

		downRouter := db.NewReadRouter(primaryDB, downDB)
		downRepo := NewPlanRepository(downRouter)

		primaryMock.ExpectQuery(listQuery).
			WithArgs("merchant-1", 10, 0).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
				AddRow("plan-3", "Scale", "3000", "USD", "month", "Scale description", "merchant-1", time.Now(), time.Now()))

		plans, err := downRepo.GetByMerchantID(context.Background(), "merchant-1", 10, 0)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		assert.Equal(t, "plan-3", plans[0].ID)

		assert.NoError(t, primaryMock.ExpectationsWereMet())
	})
}

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
