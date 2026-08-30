package handlers

import (
	"net/http"
	"strings"
	"time"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/featureflags"

	"github.com/gin-gonic/gin"
)

// FeatureFlagsHandler encapsulates feature flag management endpoints.
type FeatureFlagsHandler struct {
	flagManager *featureflags.Manager
}

// NewFeatureFlagsHandler builds a feature flags handler.
func NewFeatureFlagsHandler(flagManager *featureflags.Manager) *FeatureFlagsHandler {
	return &FeatureFlagsHandler{flagManager: flagManager}
}

// GetFeatureFlags returns all current feature flags.
func (h *FeatureFlagsHandler) GetFeatureFlags(c *gin.Context) {
	flags := h.flagManager.GetAllFlags()
	c.JSON(http.StatusOK, flags)
}

// ToggleFeatureFlagRequest represents the request body for toggling a feature flag.
type ToggleFeatureFlagRequest struct {
	Name   string `json:"name" binding:"required"`
	Reason string `json:"reason" binding:"required"`

}

// ToggleFeatureFlag toggles a feature flag's enabled state.
func (h *FeatureFlagsHandler) ToggleFeatureFlag(c *gin.Context) {
	var req ToggleFeatureFlagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid request body")
		return
	}

	// Check if flag exists
	flag, exists := h.flagManager.GetFlag(req.Name)
	if !exists {
		RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, "flag not found")
		return
	}

	// Get before state
	beforeEnabled := flag.Enabled

	// Toggle and update flag
	afterEnabled := !beforeEnabled
	newVersion := time.Now().UnixNano()

	success := h.flagManager.SetFlagWithVersion(req.Name, afterEnabled, flag.Description, newVersion)
	if !success {
		RespondWithError(c, http.StatusConflict, ErrorCodeConflict, "concurrent modification: flag was updated by another request")
		return
	}

	// Get updated flag
	updatedFlag, _ := h.flagManager.GetFlag(req.Name)

	isSensitive := false
	lowerName := strings.ToLower(req.Name)
	sensitiveKeys := []string{"token", "password", "key", "auth", "cvv", "card"}
	for _, sk := range sensitiveKeys {
		if strings.Contains(lowerName, sk) {
			isSensitive = true
			break
		}
	}

	beforeStr := boolToString(beforeEnabled)
	afterStr := boolToString(afterEnabled)
	if isSensitive {
		beforeStr = "[REDACTED]"
		afterStr = "[REDACTED]"
	}

	// Log audit action (failure doesn't block success)
	audit.LogAction(c, "feature_flag_toggle", req.Name, "success", map[string]string{
		"before_enabled": beforeStr,
		"after_enabled": afterStr,
		"reason": req.Reason,
	})

	c.JSON(http.StatusOK, updatedFlag)
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
