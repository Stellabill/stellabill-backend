package outbox

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresRepository_Store(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPostgresRepository(db)

	tests := []struct {
		name          string
		event         *Event
		expectedError string
		setupMock     func()
	}{
		{
			name: "successful event storage",
			event: &Event{
				ID:            uuid.New(),
				EventType:     "user.created",
				EventData:     json.RawMessage(`{"type":"user.created","data":{"user_id":"123"},"timestamp":"2023-01-01T00:00:00Z","id":"event-123"}`),
				AggregateID:   stringPtr("user-123"),
				AggregateType: stringPtr("user"),
				OccurredAt:    time.Now(),
				Status:        StatusPending,
				RetryCount:    0,
				MaxRetries:    3,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
				Version:       1,
			},
			setupMock: func() {
				mock.ExpectExec(`INSERT INTO outbox_events \(id, event_type, event_data, aggregate_id, aggregate_type, occurred_at, status, retry_count, max_retries, next_retry_at, error_message, created_at, updated_at, version\) VALUES \(\$1, \$2, \$3, \$4, \$5, \$6, \$7, \$8, \$9, \$10, \$11, \$12, \$13, \$14\)`).
					WithArgs(
						sqlmock.AnyArg(),
						"user.created",
						[]byte(`{"type":"user.created","data":{"user_id":"123"},"timestamp":"2023-01-01T00:00:00Z","id":"event-123"}`),
						"user-123",
						"user",
						sqlmock.AnyArg(),
						StatusPending,
						0,
						3,
						nil,
						nil,
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						1,
					).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "database error during storage",
			event: &Event{
				ID:         uuid.New(),
				EventType:  "error.event",
				EventData:  json.RawMessage(`{"type":"error.event"}`),
				OccurredAt: time.Now(),
				Status:     StatusPending,
				RetryCount: 0,
				MaxRetries: 3,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				Version:    1,
			},
			expectedError: "failed to store outbox event",
			setupMock: func() {
				mock.ExpectExec(`INSERT INTO outbox_events`).
					WithArgs(
						sqlmock.AnyArg(),
						"error.event",
						[]byte(`{"type":"error.event"}`),
						nil,
						nil,
						sqlmock.AnyArg(),
						StatusPending,
						0,
						3,
						nil,
						nil,
						sqlmock.AnyArg(),
						sqlmock.AnyArg(),
						1,
					).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.Store(tt.event)

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

func TestPostgresRepository_GetPendingEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPostgresRepository(db)

	tests := []struct {
		name           string
		limit          int
		expectedEvents []*Event
		expectedError  string
		setupMock      func()
	}{
		{
			name:  "successful pending events retrieval",
			limit: 10,
			expectedEvents: []*Event{
				{
					ID:            uuid.New(),
					EventType:     "user.created",
					EventData:     json.RawMessage(`{"type":"user.created"}`),
					AggregateID:   stringPtr("user-123"),
					AggregateType: stringPtr("user"),
					OccurredAt:    time.Now().Add(-1 * time.Hour),
					Status:        StatusPending,
					RetryCount:    0,
					MaxRetries:    3,
					CreatedAt:     time.Now().Add(-1 * time.Hour),
					UpdatedAt:     time.Now().Add(-1 * time.Hour),
					Version:       1,
				},
			},
			setupMock: func() {
				rows := sqlmock.NewRows([]string{"id", "event_type", "event_data", "aggregate_id", "aggregate_type", "occurred_at", "status", "retry_count", "max_retries", "next_retry_at", "error_message", "created_at", "updated_at", "version"}).
					AddRow(uuid.New(), "user.created", []byte(`{"type":"user.created"}`), "user-123", "user", time.Now().Add(-1*time.Hour), StatusPending, 0, 3, nil, nil, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour), 1)
				mock.ExpectQuery(`SELECT id, event_type, event_data, aggregate_id, aggregate_type, occurred_at, status, retry_count, max_retries, next_retry_at, error_message, created_at, updated_at, version FROM outbox_events WHERE status = \$1 OR \(status = \$2 AND next_retry_at <= \$3\) ORDER BY occurred_at ASC LIMIT \$4`).
					WithArgs(StatusPending, StatusFailed, sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
		},
		{
			name:          "database error during retrieval",
			limit:         10,
			expectedError: "failed to get pending events",
			setupMock: func() {
				mock.ExpectQuery(`SELECT id, event_type, event_data, aggregate_id, aggregate_type, occurred_at, status, retry_count, max_retries, next_retry_at, error_message, created_at, updated_at, version FROM outbox_events WHERE status = \$1 OR \(status = \$2 AND next_retry_at <= \$3\) ORDER BY occurred_at ASC LIMIT \$4`).
					WithArgs(StatusPending, StatusFailed, sqlmock.AnyArg(), 10).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			events, err := repo.GetPendingEvents(tt.limit)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, events)
			} else {
				assert.NoError(t, err)
				assert.Len(t, events, len(tt.expectedEvents))
				for i, expectedEvent := range tt.expectedEvents {
					assert.Equal(t, expectedEvent.EventType, events[i].EventType)
					assert.Equal(t, expectedEvent.Status, events[i].Status)
					if expectedEvent.AggregateID != nil {
						require.NotNil(t, events[i].AggregateID)
						assert.Equal(t, *expectedEvent.AggregateID, *events[i].AggregateID)
					} else {
						assert.Nil(t, events[i].AggregateID)
					}
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresRepository_MarkAsProcessing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPostgresRepository(db)

	tests := []struct {
		name          string
		eventID       uuid.UUID
		expectedError string
		setupMock     func()
	}{
		{
			name:    "successful marking as processing",
			eventID: uuid.New(),
			setupMock: func() {
				mock.ExpectExec(`UPDATE outbox_events SET status = \$1, updated_at = \$2 WHERE id = \$3 AND status = \$4`).
					WithArgs(StatusProcessing, sqlmock.AnyArg(), sqlmock.AnyArg(), StatusPending).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name:          "event not found or not in pending status",
			eventID:       uuid.New(),
			expectedError: "event not found or not in pending status",
			setupMock: func() {
				mock.ExpectExec(`UPDATE outbox_events SET status = \$1, updated_at = \$2 WHERE id = \$3 AND status = \$4`).
					WithArgs(StatusProcessing, sqlmock.AnyArg(), sqlmock.AnyArg(), StatusPending).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name:          "database error during marking",
			eventID:       uuid.New(),
			expectedError: "failed to mark event as processing",
			setupMock: func() {
				mock.ExpectExec(`UPDATE outbox_events SET status = \$1, updated_at = \$2 WHERE id = \$3 AND status = \$4`).
					WithArgs(StatusProcessing, sqlmock.AnyArg(), sqlmock.AnyArg(), StatusPending).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.MarkAsProcessing(tt.eventID)

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

func TestPostgresRepository_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPostgresRepository(db)

	t.Run("scan error with invalid data type", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"id", "event_type", "event_data", "aggregate_id", "aggregate_type", "occurred_at", "status", "retry_count", "max_retries", "next_retry_at", "error_message", "created_at", "updated_at", "version"}).
			AddRow(123, "invalid.event", []byte(`{"type":"invalid.event"}`), nil, nil, time.Now(), StatusPending, 0, 3, nil, nil, time.Now(), time.Now(), 1)

		mock.ExpectQuery(`SELECT`).WillReturnRows(rows)

		events, err := repo.GetPendingEvents(10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to scan event")
		assert.Nil(t, events)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestNewEvent(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		data          interface{}
		aggregateID   *string
		aggregateType *string
		expectedError string
	}{
		{
			name:          "successful event creation",
			eventType:     "user.created",
			data:          map[string]interface{}{"user_id": "123", "email": "test@example.com"},
			aggregateID:   stringPtr("user-123"),
			aggregateType: stringPtr("user"),
		},
		{
			name:          "successful event creation without aggregate",
			eventType:     "system.started",
			data:          map[string]interface{}{"timestamp": time.Now()},
			aggregateID:   nil,
			aggregateType: nil,
		},
		{
			name:          "error with unmarshalable data",
			eventType:     "invalid.event",
			data:          make(chan int), // channels cannot be marshaled to JSON
			aggregateID:   nil,
			aggregateType: nil,
			expectedError: "failed to marshal event data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := NewEvent(tt.eventType, tt.data, tt.aggregateID, tt.aggregateType)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, event)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, event.ID)
				assert.Equal(t, tt.eventType, event.EventType)
				assert.Equal(t, StatusPending, event.Status)
				assert.Equal(t, 0, event.RetryCount)
				assert.Equal(t, 3, event.MaxRetries)
				assert.Equal(t, 1, event.Version)
				assert.NotZero(t, event.CreatedAt)
				assert.NotZero(t, event.UpdatedAt)
				assert.NotZero(t, event.OccurredAt)

				if tt.aggregateID != nil {
					require.NotNil(t, event.AggregateID)
					assert.Equal(t, *tt.aggregateID, *event.AggregateID)
				} else {
					assert.Nil(t, event.AggregateID)
				}

				if tt.aggregateType != nil {
					require.NotNil(t, event.AggregateType)
					assert.Equal(t, *tt.aggregateType, *event.AggregateType)
				} else {
					assert.Nil(t, event.AggregateType)
				}

				// Verify event data structure
				var eventData EventData
				err = json.Unmarshal(event.EventData, &eventData)
				require.NoError(t, err)
				assert.Equal(t, tt.eventType, eventData.Type)
				assert.Equal(t, tt.data, eventData.Data)
				assert.NotEmpty(t, eventData.ID)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

