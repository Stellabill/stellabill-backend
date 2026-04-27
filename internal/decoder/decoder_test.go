package decoder

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func setupDecodeRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		var p testPayload
		if err := DecodeStrict(c, &p); err != nil {
			return
		}
		c.JSON(http.StatusOK, p)
	})
	return r
}

func doPost(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestDecodeStrict_ValidPayload(t *testing.T) {
	r := setupDecodeRouter()
	w := doPost(r, `{"name":"alice","count":3}`)
	assert.Equal(t, http.StatusOK, w.Code)

	var got testPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "alice", got.Name)
	assert.Equal(t, 3, got.Count)
}

func TestDecodeStrict_UnknownField(t *testing.T) {
	r := setupDecodeRouter()
	w := doPost(r, `{"name":"alice","count":1,"extra":"bad"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp DecodeErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "UNKNOWN_FIELD", resp.Code)
	assert.Contains(t, resp.Message, "extra")
}

func TestDecodeStrict_WrongType(t *testing.T) {
	r := setupDecodeRouter()
	// count should be int, sending string
	w := doPost(r, `{"name":"alice","count":"not-a-number"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp DecodeErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_FIELD_TYPE", resp.Code)
}

func TestDecodeStrict_MalformedJSON(t *testing.T) {
	r := setupDecodeRouter()
	w := doPost(r, `{name: alice}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp DecodeErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_JSON", resp.Code)
}

func TestDecodeStrict_EmptyBody(t *testing.T) {
	r := setupDecodeRouter()
	w := doPost(r, ``)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp DecodeErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_JSON", resp.Code)
}

func TestDecodeStrict_TrailingData(t *testing.T) {
	r := setupDecodeRouter()
	w := doPost(r, `{"name":"alice","count":1}{"extra":true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp DecodeErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_JSON", resp.Code)
}

func TestDecodeStrict_NullFields(t *testing.T) {
	// null for a string field is valid JSON — Go decodes it as zero value.
	r := setupDecodeRouter()
	w := doPost(r, `{"name":null,"count":0}`)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDecodeStrict_BodyRestoredAfterRead(t *testing.T) {
	// Verify the body is restored so downstream middleware can re-read it.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var secondRead []byte
	r.POST("/restore", func(c *gin.Context) {
		var p testPayload
		if err := DecodeStrict(c, &p); err != nil {
			return
		}
		secondRead, _ = io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	body := `{"name":"bob","count":2}`
	req := httptest.NewRequest(http.MethodPost, "/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, body, string(secondRead))
}

func TestDecodeStrict_ReadBodyError(t *testing.T) {
	// Simulate a broken reader.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/broken", func(c *gin.Context) {
		var p testPayload
		err := DecodeStrict(c, &p)
		assert.Error(t, err)
	})

	req := httptest.NewRequest(http.MethodPost, "/broken", &brokenReader{})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// brokenReader always returns an error on Read.
type brokenReader struct{}

func (b *brokenReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestIsUnknownFieldError(t *testing.T) {
	type dummy struct{ X int }
	dec := json.NewDecoder(bytes.NewReader([]byte(`{"X":1,"unknown":2}`)))
	dec.DisallowUnknownFields()
	var d dummy
	err := dec.Decode(&d)
	require.Error(t, err)
	assert.True(t, isUnknownFieldError(err))
}

func TestIsUnknownFieldError_Nil(t *testing.T) {
	assert.False(t, isUnknownFieldError(nil))
}

func TestIsUnknownFieldError_OtherError(t *testing.T) {
	assert.False(t, isUnknownFieldError(io.EOF))
}
