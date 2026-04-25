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

func TestSubscriptionRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewSubscriptionRepository(db)

	tests := []struct {
		name          string
		subscription  *Subscription
		expectedError string
		setupMock     func()
	}{
		{
			name: "successful subscription creation",
			subscription: &Subscription{
				PlanID:             "plan-123",
				CustomerID:         "customer-123",
				MerchantID:         "merchant-123",
				Status:             "active",
				Amount:             "29.99",
				Currency:           "USD",
				Interval:           "month",
				CurrentPeriodStart: time.Now().Add(-30 * 24 * time.Hour),
				CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
				CancelAtPeriodEnd:  false,
			},
			setupMock: func() {
				mock.ExpectQuery(`INSERT INTO subscriptions`).
					WithArgs(sqlmock.AnyArg(), "plan-123", "customer-123", "merchant-123", "active", "29.99", "USD", "month", sqlmock.AnyArg(), sqlmock.AnyArg(), false, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
			},
		},
		{
			name: "database error during creation",
			subscription: &Subscription{
				PlanID:             "plan-123",
				CustomerID:         "customer-123",
				MerchantID:         "merchant-123",
				Status:             "active",
				Amount:             "29.99",
				Currency:           "USD",
				Interval:           "month",
				CurrentPeriodStart: time.Now().Add(-30 * 24 * time.Hour),
				CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
				CancelAtPeriodEnd:  false,
			},
			expectedError: "failed to create subscription",
			setupMock: func() {
				mock.ExpectQuery(`INSERT INTO subscriptions`).
					WithArgs(sqlmock.AnyArg(), "plan-123", "customer-123", "merchant-123", "active", "29.99", "USD", "month", sqlmock.AnyArg(), sqlmock.AnyArg(), false, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.Create(tt.subscription)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, tt.subscription.ID)
				assert.NotZero(t, tt.subscription.CreatedAt)
				assert.NotZero(t, tt.subscription.UpdatedAt)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewSubscriptionRepository(db)

	tests := []struct {
		name                 string
		subscriptionID       string
		expectedSubscription *Subscription
		expectedError        string
		setupMock            func()
	}{
		{
			name:           "successful subscription retrieval",
			subscriptionID: "sub-123",
			expectedSubscription: &Subscription{
				ID:                 "sub-123",
				PlanID:             "plan-123",
				CustomerID:         "customer-123",
				MerchantID:         "merchant-123",
				Status:             "active",
				Amount:             "29.99",
				Currency:           "USD",
				Interval:           "month",
				CurrentPeriodStart: time.Now().Add(-30 * 24 * time.Hour),
				CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
				CancelAtPeriodEnd:  false,
			},
			setupMock: func() {
				rows := sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
					AddRow("sub-123", "plan-123", "customer-123", "merchant-123", "active", "29.99", "USD", "month", time.Now().Add(-30*24*time.Hour), time.Now().Add(30*24*time.Hour), false, nil, nil, nil, nil, time.Now(), time.Now())
				mock.ExpectQuery(`SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval, current_period_start, current_period_end, cancel_at_period_end, canceled_at, ended_at, trial_start, trial_end, created_at, updated_at FROM subscriptions WHERE id = \$1`).
					WithArgs("sub-123").
					WillReturnRows(rows)
			},
		},
		{
			name:           "subscription not found",
			subscriptionID: "nonexistent",
			expectedError:  "subscription not found",
			setupMock: func() {
				mock.ExpectQuery(`SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval, current_period_start, current_period_end, cancel_at_period_end, canceled_at, ended_at, trial_start, trial_end, created_at, updated_at FROM subscriptions WHERE id = \$1`).
					WithArgs("nonexistent").
					WillReturnError(sql.ErrNoRows)
			},
		},
		{
			name:           "database error during retrieval",
			subscriptionID: "sub-error",
			expectedError:  "failed to get subscription",
			setupMock: func() {
				mock.ExpectQuery(`SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval, current_period_start, current_period_end, cancel_at_period_end, canceled_at, ended_at, trial_start, trial_end, created_at, updated_at FROM subscriptions WHERE id = \$1`).
					WithArgs("sub-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			subscription, err := repo.GetByID(tt.subscriptionID)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, subscription)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedSubscription.ID, subscription.ID)
				assert.Equal(t, tt.expectedSubscription.PlanID, subscription.PlanID)
				assert.Equal(t, tt.expectedSubscription.CustomerID, subscription.CustomerID)
				assert.Equal(t, tt.expectedSubscription.MerchantID, subscription.MerchantID)
				assert.Equal(t, tt.expectedSubscription.Status, subscription.Status)
				assert.Equal(t, tt.expectedSubscription.Amount, subscription.Amount)
				assert.Equal(t, tt.expectedSubscription.Currency, subscription.Currency)
				assert.Equal(t, tt.expectedSubscription.Interval, subscription.Interval)
				assert.Equal(t, tt.expectedSubscription.CancelAtPeriodEnd, subscription.CancelAtPeriodEnd)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionRepository_UpdateStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewSubscriptionRepository(db)

	tests := []struct {
		name           string
		subscriptionID string
		status         string
		expectedError  string
		setupMock      func()
	}{
		{
			name:           "successful status update",
			subscriptionID: "sub-123",
			status:         "canceled",
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET status = \$1, updated_at = \$2 WHERE id = \$3`).
					WithArgs("canceled", sqlmock.AnyArg(), "sub-123").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:           "subscription not found during status update",
			subscriptionID: "nonexistent",
			status:         "canceled",
			expectedError:  "subscription not found",
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET status = \$1, updated_at = \$2 WHERE id = \$3`).
					WithArgs("canceled", sqlmock.AnyArg(), "nonexistent").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name:           "database error during status update",
			subscriptionID: "sub-error",
			status:         "canceled",
			expectedError:  "failed to update subscription status",
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET status = \$1, updated_at = \$2 WHERE id = \$3`).
					WithArgs("canceled", sqlmock.AnyArg(), "sub-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.UpdateStatus(tt.subscriptionID, tt.status)

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

func TestSubscriptionRepository_Cancel(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewSubscriptionRepository(db)

	tests := []struct {
		name              string
		subscriptionID    string
		cancelAtPeriodEnd bool
		expectedError     string
		setupMock         func()
	}{
		{
			name:              "successful immediate cancellation",
			subscriptionID:    "sub-123",
			cancelAtPeriodEnd: false,
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
					WithArgs(false, sqlmock.AnyArg(), sqlmock.AnyArg(), "sub-123").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:              "successful cancellation at period end",
			subscriptionID:    "sub-456",
			cancelAtPeriodEnd: true,
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
					WithArgs(true, sqlmock.AnyArg(), sqlmock.AnyArg(), "sub-456").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:           "subscription not found during cancellation",
			subscriptionID: "nonexistent",
			expectedError:  "subscription not found",
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
					WithArgs(false, sqlmock.AnyArg(), sqlmock.AnyArg(), "nonexistent").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name:           "database error during cancellation",
			subscriptionID: "sub-error",
			expectedError:  "failed to cancel subscription",
			setupMock: func() {
				mock.ExpectExec(`UPDATE subscriptions SET cancel_at_period_end = \$1, canceled_at = \$2, updated_at = \$3 WHERE id = \$4`).
					WithArgs(false, sqlmock.AnyArg(), sqlmock.AnyArg(), "sub-error").
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.Cancel(tt.subscriptionID, tt.cancelAtPeriodEnd)

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

func TestSubscriptionRepository_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewSubscriptionRepository(db)

	t.Run("scan error with invalid data type", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
			AddRow(123, "plan-123", "customer-123", "merchant-123", "active", "29.99", "USD", "month", time.Now().Add(-30*24*time.Hour), time.Now().Add(30*24*time.Hour), false, nil, nil, nil, nil, time.Now(), time.Now())

		mock.ExpectQuery(`SELECT`).WillReturnRows(rows)

		subscriptions, err := repo.GetByCustomerID("customer-123", 10, 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to scan subscription")
		assert.Nil(t, subscriptions)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
