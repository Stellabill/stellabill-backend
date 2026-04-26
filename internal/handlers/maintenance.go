package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/maintenance"
)

func EnableMaintenance(c *gin.Context) {
	maintenance.Enable()
	audit.LogAction(c, "maintenance_mode", "system", "enabled", nil)
	c.JSON(http.StatusOK, gin.H{"status": "maintenance mode enabled"})
}

func DisableMaintenance(c *gin.Context) {
	maintenance.Disable()
	audit.LogAction(c, "maintenance_mode", "system", "disabled", nil)
	c.JSON(http.StatusOK, gin.H{"status": "maintenance mode disabled"})
}
