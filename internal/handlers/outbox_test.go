package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stellarbill-backend/internal/outbox"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Stub repository (implements outbox.Repository)
// ---------------------------------------------------------------------------

type stubOutboxRepo struct {
	listEvents []*outbox.Event
	listErr    error
	lastList   int

	requeueErr error
	requeued   []uuid.UUID
}

func (s *stubOutboxRepo) Store(context.Context, *outbox.Event) error {
	return errors.New("unimplemented")
}
func (s *stubOutboxRepo) GetPendingEvents(int) ([]*outbox.Event, error) { return nil, nil }
func (s *stubOutboxRepo) GetByID(uuid.UUID) (*outbox.Event, error)      { return nil, nil }
func (s *stubOutboxRepo) UpdateStatus(uuid.UUID, outbox.Status, *string) error {
	return errors.New("unimplemented")
}
func (s *stubOutboxRepo) MarkAsProcessing(uuid.UUID) error { return errors.New("unimplemented") }
func (s *stubOutboxRepo) IncrementRetryCount(uuid.UUID, time.Time, *string) error {
	return errors.New("unimplemented")
}
func (s *stubOutboxRepo) DeleteCompletedEvents(time.Time) (int64, error) { return 0, nil }
func (s *stubOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error) {
	s.lastList = limit
	return s.listEvents, s.listErr
}
func (s *stubOutboxRepo) RequeueEvent(id uuid.UUID) error {
	s.requeued = append(s.requeued, id)
	return s.requeueErr
}
func (s *stubOutboxRepo) EnsurePublisherProgressTable() error             { return nil }
func (s *stubOutboxRepo) GetPublisherProgress(string) (*uuid.UUID, error) { return nil, nil }
func (s *stubOutboxRepo) GetPendingEventsForPublisher(string, int) ([]*outbox.Event, error) {
	return nil, nil
}
func (s *stubOutboxRepo) MarkPublished(string, *outbox.Event, []string) error {
	return errors.New("unimplemented")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupOutboxAdmin(repo outbox.Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewOutboxAdminHandler(repo)
	r := gin.New()
	r.GET("/outbox/dead-letter", h.ListDeadLetteredEvents)
	r.POST("/outbox/:id/requeue", h.RequeueOutboxEvent)
	return r
}

func performRequest(t *testing.T, r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(nil))
	r.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// ListDeadLetteredEvents
// ---------------------------------------------------------------------------

func TestOutboxDeadLetter_ListHappyPath(t *testing.T) {
	eventID := uuid.New()
	repo := &stubOutboxRepo{
		listEvents: []*outbox.Event{{ID: eventID, EventType: "test.event", Status: outbox.StatusFailed}},
	}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Events []*outbox.Event `json:"events"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Events, 1)
	require.Equal(t, eventID, body.Events[0].ID)
	require.Equal(t, defaultDeadLetterLimit, repo.lastList)
}

func TestOutboxDeadLetter_ListRespectsLimit(t *testing.T) {
	repo := &stubOutboxRepo{}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter?limit=5")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 5, repo.lastList)
}

func TestOutboxDeadLetter_ListCapsLimitAtMax(t *testing.T) {
	repo := &stubOutboxRepo{}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter?limit=99999")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, maxDeadLetterLimit, repo.lastList)
}

func TestOutboxDeadLetter_ListInvalidLimit(t *testing.T) {
	repo := &stubOutboxRepo{}
	r := setupOutboxAdmin(repo)

	for _, bad := range []string{"0", "-1", "abc"} {
		rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter?limit="+bad)
		require.Equal(t, http.StatusBadRequest, rec.Code, "limit=%q", bad)
	}
	require.Equal(t, 0, repo.lastList, "repo should not be called for invalid limits")
}

func TestOutboxDeadLetter_ListRepoError(t *testing.T) {
	repo := &stubOutboxRepo{listErr: errors.New("boom")}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter")
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestOutboxDeadLetter_ListRepoUnavailable(t *testing.T) {
	r := setupOutboxAdmin(nil)

	rec := performRequest(t, r, http.MethodGet, "/outbox/dead-letter")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ---------------------------------------------------------------------------
// RequeueOutboxEvent
// ---------------------------------------------------------------------------

func TestOutboxDeadLetter_RequeueHappyPath(t *testing.T) {
	id := uuid.New()
	repo := &stubOutboxRepo{}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodPost, "/outbox/"+id.String()+"/requeue")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []uuid.UUID{id}, repo.requeued)
}

func TestOutboxDeadLetter_RequeueInvalidID(t *testing.T) {
	repo := &stubOutboxRepo{}
	r := setupOutboxAdmin(repo)

	rec := performRequest(t, r, http.MethodPost, "/outbox/not-a-uuid/requeue")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, repo.requeued)
}

func TestOutboxDeadLetter_RequeueNotFound(t *testing.T) {
	repo := &stubOutboxRepo{requeueErr: errors.New("event not found or not in failed status")}
	r := setupOutboxAdmin(repo)

	id := uuid.New()
	rec := performRequest(t, r, http.MethodPost, "/outbox/"+id.String()+"/requeue")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOutboxDeadLetter_RequeueInternalError(t *testing.T) {
	repo := &stubOutboxRepo{requeueErr: errors.New("db down")}
	r := setupOutboxAdmin(repo)

	id := uuid.New()
	rec := performRequest(t, r, http.MethodPost, "/outbox/"+id.String()+"/requeue")
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestOutboxDeadLetter_RequeueRepoUnavailable(t *testing.T) {
	r := setupOutboxAdmin(nil)

	rec := performRequest(t, r, http.MethodPost, "/outbox/"+uuid.New().String()+"/requeue")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
