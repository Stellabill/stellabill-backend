package repositories

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPlanRepository(db)

	tests := []struct {
		name          string
		plan          *Plan
		expectedError string
		setupMock     func()
	}{
		{
			name: "successful plan creation",
			plan: &Plan{
				Name:        "Premium Plan",
				Amount:      "29.99",
				Currency:    "USD",
				Interval:    "month",
				Description: stringPtr("Premium monthly plan"),
				MerchantID:  "merchant-123",
			},
			setupMock: func() {
				mock.ExpectQuery(`INSERT INTO plans`).
					WithArgs(sqlmock.AnyArg(), "Premium Plan", "29.99", "USD", "month", "Premium monthly plan", "merchant-123", sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
			},
		},
		{
			name: "plan creation with null description",
			plan: &Plan{
				Name:       "Basic Plan",
				Amount:     "9.99",
				Currency:   "USD",
				Interval:   "month",
				MerchantID: "merchant-123",
			},
			setupMock: func() {
				mock.ExpectQuery(`INSERT INTO plans`).
					WithArgs(sqlmock.AnyArg(), "Basic Plan", "9.99", "USD", "month", nil, "merchant-123", sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
			},
		},
		{
			name: "database error during creation",
			plan: &Plan{
				Name:       "Error Plan",
				Amount:     "19.99",
				Currency:   "USD",
				Interval:   "month",
				MerchantID: "merchant-123",
			},
			expectedError: "failed to create plan",
			setupMock: func() {
				mock.ExpectQuery(`INSERT INTO plans`).
					WithArgs(sqlmock.AnyArg(), "Error Plan", "19.99", "USD", "month", nil, "merchant-123", sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			
			err := repo.Create(tt.plan)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, tt.plan.ID)
				assert.NotZero(t, tt.plan.CreatedAt)
				assert.NotZero(t, tt.plan.UpdatedAt)
			}
			
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPlanRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPlanRepository(db)

	tests := []struct {
		name          string
		planID        string
		expectedPlan  *Plan
		expectedError string
		setupMock     func()
	}{
		{
			name:   "successful plan retrieval",
			planID: "plan-123",
			expectedPlan: &Plan{
				ID:          "plan-123",
				Name:        "Premium Plan",
				Amount:      "29.99",
				Currency:    "USD",
				Interval:    "month",
				Description: stringPtr("Premium monthly plan"),
				MerchantID:  "merchant-123",
			},
			setupMock: func() {
				rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
					AddRow("plan-123", "Premium Plan", "29.99", "USD", "month", "Premium monthly plan", "merchant-123", time.Now(), time.Now())
				mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \$1`).
					WithArgs("plan-123").
					WillReturnRows(rows)
			},
		},
		{
			name:   "plan retrieval with null description",
			planID: "plan-456",
			expectedPlan: &Plan{
				ID:         "plan-456",
				Name:       "Basic Plan",
				Amount:     "9.99",
				Currency:   "USD",
				Interval:   "month",
				MerchantID: "merchant-123",
			},
			setupMock: func() {
				rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
					AddRow("plan-456", "Basic Plan", "9.99", "USD", "month", nil, "merchant-123", time.Now(), time.Now())
				mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \$1`).
					WithArgs("plan-456").
					WillReturnRows(rows)
			},
		},
		{
			name:          "plan not found",
			planID:        "nonexistent",
			expectedError: "plan not found",
			setupMock: func() {
				mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \$1`).
					WithArgs("nonexistent").
					WillReturnError(sql.ErrNoRows)
			},
		},
		{
			name:          "database error during retrieval",
			planID:        "plan-error",
			expectedError: "failed to get plan",
			setupMock: func() {
				mock.ExpectQuery(`SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at FROM plans WHERE id = \$1`).
					WithArgs("plan-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			
			plan, err := repo.GetByID(tt.planID)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, plan)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPlan.ID, plan.ID)
				assert.Equal(t, tt.expectedPlan.Name, plan.Name)
				assert.Equal(t, tt.expectedPlan.Amount, plan.Amount)
				assert.Equal(t, tt.expectedPlan.Currency, plan.Currency)
				assert.Equal(t, tt.expectedPlan.Interval, plan.Interval)
				assert.Equal(t, tt.expectedPlan.MerchantID, plan.MerchantID)
				if tt.expectedPlan.Description != nil {
					require.NotNil(t, plan.Description)
					assert.Equal(t, *tt.expectedPlan.Description, *plan.Description)
				} else {
					assert.Nil(t, plan.Description)
				}
			}
			
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPlanRepository_Update(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPlanRepository(db)

	tests := []struct {
		name          string
		plan          *Plan
		expectedError string
		setupMock     func()
	}{
		{
			name: "successful plan update",
			plan: &Plan{
				ID:          "plan-123",
				Name:        "Updated Plan",
				Amount:      "39.99",
				Currency:    "USD",
				Interval:    "month",
				Description: stringPtr("Updated description"),
				MerchantID:  "merchant-123",
			},
			setupMock: func() {
				mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
					WithArgs("Updated Plan", "39.99", "USD", "month", "Updated description", sqlmock.AnyArg(), "plan-123").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "plan not found during update",
			plan: &Plan{
				ID:         "nonexistent",
				Name:       "Nonexistent Plan",
				Amount:     "19.99",
				Currency:   "USD",
				Interval:   "month",
				MerchantID: "merchant-123",
			},
			expectedError: "plan not found",
			setupMock: func() {
				mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
					WithArgs("Nonexistent Plan", "19.99", "USD", "month", nil, sqlmock.AnyArg(), "nonexistent").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name: "database error during update",
			plan: &Plan{
				ID:         "plan-error",
				Name:       "Error Plan",
				Amount:     "19.99",
				Currency:   "USD",
				Interval:   "month",
				MerchantID: "merchant-123",
			},
			expectedError: "failed to update plan",
			setupMock: func() {
				mock.ExpectExec(`UPDATE plans SET name = \$1, amount = \$2, currency = \$3, interval = \$4, description = \$5, updated_at = \$6 WHERE id = \$7`).
					WithArgs("Error Plan", "19.99", "USD", "month", nil, sqlmock.AnyArg(), "plan-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			
			err := repo.Update(tt.plan)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotZero(t, tt.plan.UpdatedAt)
			}
			
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPlanRepository_Delete(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPlanRepository(db)

	tests := []struct {
		name          string
		planID        string
		expectedError string
		setupMock     func()
	}{
		{
			name:   "successful plan deletion",
			planID: "plan-123",
			setupMock: func() {
				mock.ExpectExec(`DELETE FROM plans WHERE id = \$1`).
					WithArgs("plan-123").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:          "plan not found during deletion",
			planID:        "nonexistent",
			expectedError: "plan not found",
			setupMock: func() {
				mock.ExpectExec(`DELETE FROM plans WHERE id = \$1`).
					WithArgs("nonexistent").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name:          "database error during deletion",
			planID:        "plan-error",
			expectedError: "failed to delete plan",
			setupMock: func() {
				mock.ExpectExec(`DELETE FROM plans WHERE id = \$1`).
					WithArgs("plan-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			
			err := repo.Delete(tt.planID)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
			
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPlanRepository_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPlanRepository(db)

	t.Run("scan error with invalid data type", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "name", "amount", "currency", "interval", "description", "merchant_id", "created_at", "updated_at"}).
			AddRow(123, "Invalid Plan", "29.99", "USD", "month", "Description", "merchant-123", time.Now(), time.Now())
		
		mock.ExpectQuery(`SELECT`).WillReturnRows(rows)
		
		plans, err := repo.GetByMerchantID("merchant-123", 10, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to scan plan")
		assert.Nil(t, plans)
		
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func stringPtr(s string) *string {
	return &s
}
