package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/featureflags"
	"stellarbill-backend/internal/middleware"
)

func TestFeatureFlagMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	t.Run("allows request when flag is enabled", func(t *testing.T) {
		featureflags.SetFlag("test_flag", true, "test")
		r := gin.New()
		r.Use(middleware.FeatureFlag("test_flag"))
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("blocks request when flag is disabled", func(t *testing.T) {
		featureflags.SetFlag("test_flag", false, "test")
		r := gin.New()
		r.Use(middleware.FeatureFlag("test_flag"))
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}
