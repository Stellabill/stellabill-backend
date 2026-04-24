package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/repositories"
)

func TestUpdateSubscriptionStatus_Atomicity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Fail after domain write - Verify rollback", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		outboxSvc, _ := outbox.NewService(db, outbox.ServiceConfig{})
		h := &Handler{
			DB:      db,
			SubRepo: repositories.NewSubscriptionRepository(db),
			Outbox:  outboxSvc,
		}

		subID := "550e8400-e29b-41d4-a716-446655440000"
		newStatus := "cancelled"

		// Expectations
		mock.ExpectBegin()
		// 1. Fetch current subscription
		rows := sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
			AddRow(subID, "plan-1", "cust-1", "merch-1", "active", "100", "USD", "monthly", time.Now(), time.Now(), false, nil, nil, nil, nil, time.Now(), time.Now())
		mock.ExpectQuery(`SELECT .* FROM subscriptions WHERE id = \$1`).WithArgs(subID).WillReturnRows(rows)
		
		// 2. Update status
		mock.ExpectExec(`UPDATE subscriptions SET status = \$1`).WithArgs(newStatus, sqlmock.AnyArg(), subID).WillReturnResult(sqlmock.NewResult(0, 1))
		
		// 3. Publish outbox event - FAIL HERE
		mock.ExpectExec(`INSERT INTO outbox_events`).WillReturnError(fmt.Errorf("outbox storage failed"))
		
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: subID}}
		
		payload := map[string]string{"status": newStatus}
		jsonPayload, _ := json.Marshal(payload)
		c.Request, _ = http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(jsonPayload))
		
		h.UpdateSubscriptionStatus(c)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Fail after outbox write - Verify rollback", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		outboxSvc, _ := outbox.NewService(db, outbox.ServiceConfig{})
		h := &Handler{
			DB:      db,
			SubRepo: repositories.NewSubscriptionRepository(db),
			Outbox:  outboxSvc,
		}

		subID := "660e8400-e29b-41d4-a716-446655440000"
		newStatus := "cancelled"

		// Expectations
		mock.ExpectBegin()
		// 1. Fetch current subscription
		rows := sqlmock.NewRows([]string{"id", "plan_id", "customer_id", "merchant_id", "status", "amount", "currency", "interval", "current_period_start", "current_period_end", "cancel_at_period_end", "canceled_at", "ended_at", "trial_start", "trial_end", "created_at", "updated_at"}).
			AddRow(subID, "plan-1", "cust-1", "merch-1", "active", "100", "USD", "monthly", time.Now(), time.Now(), false, nil, nil, nil, nil, time.Now(), time.Now())
		mock.ExpectQuery(`SELECT .* FROM subscriptions WHERE id = \$1`).WithArgs(subID).WillReturnRows(rows)
		
		// 2. Update status
		mock.ExpectExec(`UPDATE subscriptions SET status = \$1`).WithArgs(newStatus, sqlmock.AnyArg(), subID).WillReturnResult(sqlmock.NewResult(0, 1))
		
		// 3. Publish outbox event
		mock.ExpectExec(`INSERT INTO outbox_events`).WillReturnResult(sqlmock.NewResult(0, 1))
		
		// 4. Commit - FAIL HERE
		mock.ExpectCommit().WillReturnError(fmt.Errorf("commit failed"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: subID}}
		
		payload := map[string]string{"status": newStatus}
		jsonPayload, _ := json.Marshal(payload)
		c.Request, _ = http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(jsonPayload))
		c.Request.Header.Set("Content-Type", "application/json")
		
		h.UpdateSubscriptionStatus(c)

		// Note: RunInTransaction returns the commit error
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// Add a dummy time to satisfy requirements if needed, but time is imported
var _ = time.Now
