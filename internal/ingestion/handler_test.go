package ingestion

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRouter() (*gin.Engine, *Service, *MockRepository) {
	gin.SetMode(gin.TestMode)
	repo := NewMockRepository()
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	svc := NewService(repo, logger)

	r := gin.New()
	r.POST("/api/contract-events", NewIngestHandler(svc))
	r.GET("/api/contracts/:contract_id/events", NewListByContractHandler(repo))
	return r, svc, repo
}

func postJSON(r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIngestHandler_Success(t *testing.T) {
	r, _, _ := setupRouter()
	raw := validRawEvent()
	w := postJSON(r, "/api/contract-events", raw)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "contract-abc", data["contract_id"])
	assert.Equal(t, "processed", data["status"])
}

func TestIngestHandler_InvalidJSON(t *testing.T) {
	r, _, _ := setupRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/contract-events", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIngestHandler_MissingField(t *testing.T) {
	r, _, _ := setupRouter()
	raw := validRawEvent()
	raw.ContractID = ""
	w := postJSON(r, "/api/contract-events", raw)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "contract_id")
}

func TestIngestHandler_DuplicateReturns409(t *testing.T) {
	r, _, _ := setupRouter()
	raw := validRawEvent()

	w1 := postJSON(r, "/api/contract-events", raw)
	assert.Equal(t, http.StatusCreated, w1.Code)

	w2 := postJSON(r, "/api/contract-events", raw)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

func TestIngestHandler_OutOfOrderReturns409(t *testing.T) {
	r, _, _ := setupRouter()

	raw := validRawEvent()
	raw.SequenceNum = 5
	raw.IdempotencyKey = "key-seq5"
	w1 := postJSON(r, "/api/contract-events", raw)
	assert.Equal(t, http.StatusCreated, w1.Code)

	raw2 := validRawEvent()
	raw2.SequenceNum = 3
	raw2.IdempotencyKey = "key-seq3"
	w2 := postJSON(r, "/api/contract-events", raw2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

func TestIngestHandler_InvalidEventType(t *testing.T) {
	r, _, _ := setupRouter()
	raw := validRawEvent()
	raw.EventType = "bad.type"
	w := postJSON(r, "/api/contract-events", raw)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIngestHandler_FutureTimestamp(t *testing.T) {
	r, _, _ := setupRouter()
	raw := validRawEvent()
	raw.OccurredAt = time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	w := postJSON(r, "/api/contract-events", raw)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListByContractHandler_Empty(t *testing.T) {
	r, _, _ := setupRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/contracts/unknown/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Empty(t, data)
}

func TestListByContractHandler_WithEvents(t *testing.T) {
	r, _, _ := setupRouter()

	// Ingest two events for the same contract.
	for i, key := range []string{"k1", "k2"} {
		raw := validRawEvent()
		raw.IdempotencyKey = key
		raw.SequenceNum = int64(i + 1)
		w := postJSON(r, "/api/contract-events", raw)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/contracts/contract-abc/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Len(t, data, 2)
}

func TestListByContractHandler_Pagination(t *testing.T) {
	r, _, _ := setupRouter()

	// Ingest 5 events.
	for i := 1; i <= 5; i++ {
		raw := validRawEvent()
		raw.IdempotencyKey = "pk-" + string(rune('0'+i))
		raw.SequenceNum = int64(i)
		postJSON(r, "/api/contract-events", raw)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/contracts/contract-abc/events?limit=2&offset=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Len(t, data, 2)
	assert.Equal(t, float64(2), resp["limit"])
	assert.Equal(t, float64(1), resp["offset"])
}
