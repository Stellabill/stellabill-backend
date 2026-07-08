package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/service"
)

// FeesHandler handles fee-related HTTP requests.
type FeesHandler struct {
	svc       service.FeeService
	freshness service.FreshnessProvider
}

// NewFeesHandler creates a FeesHandler. The freshness provider is optional; when
// non-nil, GetFeeHistory annotates responses with the fee-revenue materialized
// view's last_refreshed_at and a stale-but-served flag.
func NewFeesHandler(svc service.FeeService, freshness service.FreshnessProvider) *FeesHandler {
	return &FeesHandler{svc: svc, freshness: freshness}
}

// GetFeeHistory godoc
// GET /api/v1/fees/history?type=<type>&from=<RFC3339>&to=<RFC3339>
func (h *FeesHandler) GetFeeHistory(c *gin.Context) {
	feeType := c.Query("type")

	fromStr := c.Query("from")
	toStr := c.Query("to")

	now := time.Now().UTC()
	from := now.AddDate(0, -1, 0) // default: last 30 days
	to := now

	if fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid 'from' date, use RFC3339", nil)
			return
		}
		from = t
	}
	if toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid 'to' date, use RFC3339", nil)
			return
		}
		to = t
	}

	if to.Before(from) {
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "'to' must be after 'from'", nil)
		return
	}

	history, err := h.svc.GetFeeHistory(feeType, from, to)
	if err != nil {
		RespondWithInternalError(c, "failed to retrieve fee history")
		return
	}

	// Annotate with materialized-view freshness when a provider is configured.
	// A freshness lookup failure must not fail the report: the data is still
	// valid, so we log-and-serve without the metadata rather than 500.
	if h.freshness != nil {
		if err := history.WithFreshness(c.Request.Context(), h.freshness, time.Now().UTC()); err != nil {
			history.LastRefreshedAt = nil
			history.Stale = false
		}
	}

	c.JSON(http.StatusOK, history)
}
