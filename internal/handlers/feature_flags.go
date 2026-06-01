package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/featureflags"
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
	Name string `json:"name" binding:"required"`
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
	h.flagManager.SetFlag(req.Name, afterEnabled, flag.Description)

	// Get updated flag
	updatedFlag, _ := h.flagManager.GetFlag(req.Name)

	// Log audit action (failure doesn't block success)
	audit.LogAction(c, "feature_flag_toggle", req.Name, "success", map[string]string{
		"before_enabled": boolToString(beforeEnabled),
		"after_enabled":  boolToString(afterEnabled),
	})

	c.JSON(http.StatusOK, updatedFlag)
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
