package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// stub store
// ---------------------------------------------------------------------------

// stubIdempotencyStore is a minimal IdempotencyStore for handler tests.
// Only Lookup is exercised by InspectKey; the remaining methods panic so any
// unexpected call is immediately visible.
type stubIdempotencyStore struct {
	record *middleware.IdempotencyRecord
	err    error
	// capturedScope and capturedKey let assertions verify tenant scoping.
	capturedScope string
	capturedKey   string
}

func (s *stubIdempotencyStore) Lookup(_ context.Context, scope, key string) (*middleware.IdempotencyRecord, error) {
	s.capturedScope = scope
	s.capturedKey = key
	return s.record, s.err
}

func (s *stubIdempotencyStore) GetOrInsert(_ context.Context, _, _, _, _, _ string, _ time.Duration) (int, []byte, bool, bool, error) {
	panic("GetOrInsert called unexpectedly in handler test")
}
func (s *stubIdempotencyStore) UpdateResponse(_ context.Context, _, _ string, _ int, _ []byte) error {
	panic("UpdateResponse called unexpectedly in handler test")
}
func (s *stubIdempotencyStore) Delete(_ context.Context, _, _ string) error {
	panic("Delete called unexpectedly in handler test")
}
func (s *stubIdempotencyStore) DeleteExpiredBatch(_ context.Context, _ int) (int64, error) {
	panic("DeleteExpiredBatch called unexpectedly in handler test")
}
func (s *stubIdempotencyStore) CountExpiredPending(_ context.Context) (int64, error) {
	panic("CountExpiredPending called unexpectedly in handler test")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newInspectContext builds a gin.Context wired to call InspectKey for the
// given key param. Optionally set tenantID / callerID in the context to
// simulate authenticated requests.
func newInspectContext(key string, contextKV ...any) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/idempotency/"+key, nil)
	c.Params = gin.Params{{Key: "key", Value: key}}
	for i := 0; i+1 < len(contextKV); i += 2 {
		c.Set(contextKV[i].(string), contextKV[i+1])
	}
	return w, c
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestInspectKey_Found(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &stubIdempotencyStore{
		record: &middleware.IdempotencyRecord{
			PayloadHash: "abc123",
			StatusCode:  201,
			ExpiresAt:   now.Add(24 * time.Hour),
			UsedAt:      now,
		},
	}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("order-abc", "tenantID", "tenant-1", "callerID", "user-42")

	h.InspectKey(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp IdempotencyInspectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "order-abc", resp.Key)
	assert.Equal(t, 201, resp.StatusCode)
	assert.Equal(t, "abc123", resp.RequestFingerprint)
	assert.WithinDuration(t, now, resp.UsedAt, time.Second)
	assert.WithinDuration(t, now.Add(24*time.Hour), resp.ExpiresAt, time.Second)

	// Scope must be tenant-1:user-42 — matching what the middleware records.
	assert.Equal(t, "tenant-1:user-42", store.capturedScope)
	assert.Equal(t, "order-abc", store.capturedKey)
}

func TestInspectKey_NotFound(t *testing.T) {
	store := &stubIdempotencyStore{record: nil, err: nil}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("missing-key", "tenantID", "tenant-1")

	h.InspectKey(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestInspectKey_StoreError(t *testing.T) {
	store := &stubIdempotencyStore{err: errors.New("db unavailable")}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("some-key")

	h.InspectKey(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestInspectKey_KeyTooLong(t *testing.T) {
	// A key longer than 255 characters must be rejected before the store is hit.
	store := &stubIdempotencyStore{}
	h := NewIdempotencyHandler(store)
	longKey := string(make([]byte, 256))
	w, c := newInspectContext(longKey)

	h.InspectKey(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	// Store must not have been touched.
	assert.Empty(t, store.capturedScope)
}

func TestInspectKey_TenantScope_TenantOnly(t *testing.T) {
	store := &stubIdempotencyStore{record: nil}
	h := NewIdempotencyHandler(store)
	// Only tenantID set — callerID absent.
	w, c := newInspectContext("k", "tenantID", "t1")
	h.InspectKey(c)

	// 404 is fine; we care about the scope string.
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "t1:anonymous", store.capturedScope)
}

func TestInspectKey_TenantScope_CallerOnly(t *testing.T) {
	store := &stubIdempotencyStore{record: nil}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("k", "callerID", "c1")
	h.InspectKey(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "anonymous:c1", store.capturedScope)
}

func TestInspectKey_TenantScope_NeitherSet(t *testing.T) {
	store := &stubIdempotencyStore{record: nil}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("k")
	h.InspectKey(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "anonymous", store.capturedScope)
}

func TestInspectKey_InFlightStatusCode(t *testing.T) {
	// status_code 0 means the original request is still in-flight; the handler
	// should return it faithfully so the caller knows what state it's in.
	now := time.Now().UTC()
	store := &stubIdempotencyStore{
		record: &middleware.IdempotencyRecord{
			PayloadHash: "deadbeef",
			StatusCode:  0,
			ExpiresAt:   now.Add(time.Hour),
			UsedAt:      now,
		},
	}
	h := NewIdempotencyHandler(store)
	w, c := newInspectContext("inflight-key", "tenantID", "t1", "callerID", "c1")

	h.InspectKey(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp IdempotencyInspectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.StatusCode)
}
