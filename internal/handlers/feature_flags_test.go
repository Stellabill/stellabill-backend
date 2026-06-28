package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/featureflags"
)

func TestNewFeatureFlagsHandler(t *testing.T) {
	manager := featureflags.NewManager()
	handler := NewFeatureFlagsHandler(manager)
	assert.NotNil(t, handler)
	assert.Equal(t, manager, handler.flagManager)
}

func TestGetFeatureFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := featureflags.NewManager()
	manager.SetFlag("test_flag", true, "Test description")
	handler := NewFeatureFlagsHandler(manager)

	r := gin.New()
	r.GET("/flags", handler.GetFeatureFlags)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/flags", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]featureflags.Flag
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "test_flag")
	assert.True(t, response["test_flag"].Enabled)
	assert.Equal(t, "Test description", response["test_flag"].Description)
}

func TestToggleFeatureFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := featureflags.NewManager()
	manager.SetFlag("test_flag", true, "Test description")
	handler := NewFeatureFlagsHandler(manager)

	r := gin.New()
	r.POST("/flags/toggle", handler.ToggleFeatureFlag)

	t.Run("Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := []byte(`{"name":"test_flag"}`)
		req, _ := http.NewRequest(http.MethodPost, "/flags/toggle", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response featureflags.Flag
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.False(t, response.Enabled) // Toggled from true to false
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := []byte(`{invalid json}`)
		req, _ := http.NewRequest(http.MethodPost, "/flags/toggle", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Missing Flag", func(t *testing.T) {
		w := httptest.NewRecorder()
		body := []byte(`{"name":"non_existent"}`)
		req, _ := http.NewRequest(http.MethodPost, "/flags/toggle", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestBoolToString(t *testing.T) {
	assert.Equal(t, "true", boolToString(true))
	assert.Equal(t, "false", boolToString(false))
}
