package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type TestQuery struct {
	Page  int `form:"page" binding:"required,min=1"`
	Limit int `form:"limit" binding:"required,min=1,max=100"`
}

func TestValidateQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid query", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", ValidateQuery[TestQuery](), func(c *gin.Context) {
			query, _ := c.Get("query")
			c.JSON(http.StatusOK, query)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test?page=1&limit=10", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TestQuery
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, 1, resp.Page)
		assert.Equal(t, 10, resp.Limit)
	})

	t.Run("invalid query", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", ValidateQuery[TestQuery](), func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test?page=0&limit=101", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp ValidationResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "validation_failed", resp.Error)
		assert.Len(t, resp.Details, 2)
	})
}

type TestPath struct {
	ID string `uri:"id" binding:"required,uuid4"`
}

func TestValidatePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid path", func(t *testing.T) {
		r := gin.New()
		r.GET("/test/:id", ValidatePath[TestPath](), func(c *gin.Context) {
			path, _ := c.Get("path")
			c.JSON(http.StatusOK, path)
		})

		w := httptest.NewRecorder()
		id := "550e8400-e29b-41d4-a716-446655440000"
		req, _ := http.NewRequest("GET", "/test/"+id, nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TestPath
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, id, resp.ID)
	})

	t.Run("invalid path", func(t *testing.T) {
		r := gin.New()
		r.GET("/test/:id", ValidatePath[TestPath](), func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test/invalid-id", nil)
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestBindAndValidate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid bind", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/test?page=1&limit=10", nil)

		var query TestQuery
		ok := BindAndValidate(c, &query)
		assert.True(t, ok)
		assert.Equal(t, 1, query.Page)
	})

	t.Run("invalid bind", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/test?page=0&limit=10", nil)

		var query TestQuery
		ok := BindAndValidate(c, &query)
		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("malformed json", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/test", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		// No body = EOF error which is not a validator error

		var data struct{ Name string }
		ok := BindAndValidate(c, &data)
		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleValidationError_OtherError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	handleValidationError(c, fmt.Errorf("some other error"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "some other error", resp["error"])
}

