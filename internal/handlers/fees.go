package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/fees"
)

// FeesHandler handles fee-related HTTP requests.
type FeesHandler struct {
	svc fees.Service
}

// NewFeesHandler creates a FeesHandler with the given service.
func NewFeesHandler(svc fees.Service) *FeesHandler {
	return &FeesHandler{svc: svc}
}

// ListFees handles GET /api/v1/fees?subscription_id=&status=&kind=
func (h *FeesHandler) ListFees(c *gin.Context) {
	subID := c.Query("subscription_id")
	if subID == "" {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "subscription_id is required")
		return
	}

	result, err := h.svc.ListFees(c.Request.Context(), subID, c.Query("status"), c.Query("kind"))
	if err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to list fees")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_version": "2025-01-01",
		"data":        gin.H{"fees": result},
	})
}

// GetFeeHistory handles GET /api/v1/fees/history?subscription_id=&limit=
func (h *FeesHandler) GetFeeHistory(c *gin.Context) {
	subID := c.Query("subscription_id")
	if subID == "" {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "subscription_id is required")
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	result, err := h.svc.GetHistory(c.Request.Context(), subID, limit)
	if err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to get fee history")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_version": "2025-01-01",
		"data":        gin.H{"history": result},
	})
}

// GetFeeTrends handles GET /api/v1/fees/trends?subscription_id=
func (h *FeesHandler) GetFeeTrends(c *gin.Context) {
	subID := c.Query("subscription_id")
	if subID == "" {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "subscription_id is required")
		return
	}

	trend, err := h.svc.GetTrends(c.Request.Context(), subID)
	if err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to get fee trends")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_version": "2025-01-01",
		"data":        trend,
	})
}
