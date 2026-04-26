package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/maintenance"
)

func TestMaintenanceModeMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("allows requests when maintenance mode is inactive", func(t *testing.T) {
		maintenance.Disable()
		router := gin.New()
		router.Use(MaintenanceMode())
		router.POST("/api/v1/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allows safe reads when maintenance mode is active", func(t *testing.T) {
		maintenance.Enable()
		defer maintenance.Disable()

		router := gin.New()
		router.Use(MaintenanceMode())
		router.GET("/api/v1/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest(http.MethodGet, "/api/v1/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("blocks mutations when maintenance mode is active", func(t *testing.T) {
		maintenance.Enable()
		defer maintenance.Disable()

		router := gin.New()
		router.Use(MaintenanceMode())
		router.POST("/api/v1/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Contains(t, w.Body.String(), "MAINTENANCE_MODE")
	})

	t.Run("allows admin endpoints when maintenance mode is active", func(t *testing.T) {
		maintenance.Enable()
		defer maintenance.Disable()

		router := gin.New()
		router.Use(MaintenanceMode())
		router.POST("/api/admin/maintenance/disable", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest(http.MethodPost, "/api/admin/maintenance/disable", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
