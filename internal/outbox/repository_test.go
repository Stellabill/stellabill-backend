package outbox

import (
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
				TenantID:      "tenant-a",
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
				mock.ExpectExec(`INSERT INTO outbox_events`).
					WithArgs(
						sqlmock.AnyArg(),
						"tenant-a",
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
						nil,
					).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "database error during storage",
			event: &Event{
				ID:         uuid.New(),
				TenantID:   "tenant-b",
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
						"tenant-b",
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
						nil,
					).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			err := repo.Store(context.Background(), tt.event)

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
					TenantID:      "tenant-c",
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
				rows := sqlmock.NewRows([]string{"id", "tenant_id", "event_type", "event_data", "aggregate_id", "aggregate_type", "occurred_at", "status", "retry_count", "max_retries", "next_retry_at", "error_message", "created_at", "updated_at", "version", "deduplication_id"}).
					AddRow(uuid.New(), "tenant-c", "user.created", []byte(`{"type":"user.created"}`), "user-123", "user", time.Now().Add(-1*time.Hour), StatusPending, 0, 3, nil, nil, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour), 1, nil)
				mock.ExpectQuery(`SELECT .* FROM outbox_events`).
					WithArgs(StatusPending, StatusFailed, sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
		},
		{
			name:          "database error during retrieval",
			limit:         10,
			expectedError: "failed to get pending events",
			setupMock: func() {
				mock.ExpectQuery(`SELECT .* FROM outbox_events`).
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
					assert.Equal(t, expectedEvent.TenantID, events[i].TenantID)
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

func TestPostgresRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewPostgresRepository(db)

	tests := []struct {
		name          string
		id            uuid.UUID
		expectedEvent *Event
		expectedError string
		setupMock     func()
	}{
		{
			name: "successful event retrieval",
			id:   uuid.New(),
			expectedEvent: &Event{
				ID:         uuid.New(),
				TenantID:   "tenant-d",
				EventType:  "user.updated",
				EventData:  json.RawMessage(`{"type":"user.updated"}`),
				OccurredAt: time.Now(),
				Status:     StatusPending,
				RetryCount: 0,
				MaxRetries: 3,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				Version:    1,
			},
			setupMock: func() {
				rows := sqlmock.NewRows([]string{"id", "tenant_id", "event_type", "event_data", "aggregate_id", "aggregate_type", "occurred_at", "status", "retry_count", "max_retries", "next_retry_at", "error_message", "created_at", "updated_at", "version", "deduplication_id"}).
					AddRow(uuid.New(), "tenant-d", "user.updated", []byte(`{"type":"user.updated"}`), nil, nil, time.Now(), StatusPending, 0, 3, nil, nil, time.Now(), time.Now(), 1, nil)
				mock.ExpectQuery(`SELECT .* FROM outbox_events`).
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(rows)
			},
		},
		{
			name:          "database error during retrieval",
			id:            uuid.New(),
			expectedError: "failed to scan event",
			setupMock: func() {
				mock.ExpectQuery(`SELECT .* FROM outbox_events`).
					WithArgs(sqlmock.AnyArg()).
					WillReturnError(fmt.Errorf("scan failure"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			event, err := repo.GetByID(tt.id)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, event)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedEvent.EventType, event.EventType)
				assert.Equal(t, tt.expectedEvent.Status, event.Status)
				assert.Equal(t, tt.expectedEvent.TenantID, event.TenantID)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
