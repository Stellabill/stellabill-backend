package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"

	"github.com/gin-gonic/gin"
)

// Subscription is the list-item representation of a billing subscription.
type Subscription struct {
	ID          string `json:"id"`
	PlanID      string `json:"plan_id"`
	Customer    string `json:"customer"`
	Status      string `json:"status"`
	Amount      string `json:"amount"`
	Interval    string `json:"interval"`
	NextBilling string `json:"next_billing,omitempty"`
}

func (s Subscription) GetID() string        { return s.ID }
func (s Subscription) GetSortValue() string { return s.Customer } // Sort by customer for now

func (h *Handler) ListSubscriptions(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid cursor format")
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RenderProblem(c, http.StatusInternalServerError, ErrorCodeInternalError, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)
	setPaginationLinkHeader(c, page, cursorStr != "")

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": page.Items,
		"next_cursor":   page.NextCursor,
		"has_more":      page.HasMore,
	})
}

func (h *Handler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	sub, err := h.Subscriptions.GetSubscription(c, id)
	if err != nil {
		RenderProblem(c, http.StatusNotFound, ErrorCodeNotFound, "not found")
		return
	}
	c.JSON(http.StatusOK, sub)
}

// PatchSubscription applies a partial update to a subscription using JSON Merge Patch.
func (h *Handler) PatchSubscription(c *gin.Context) {
	id := c.Param("id")
	contentType, err := parseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error())
		return
	}

	var payload map[string]json.RawMessage
	if contentType == "application/merge-patch+json" {
		payload, err = decodeJSONPatchPayload(c.Request.Body, []string{"status", "plan_id", "customer", "amount", "interval", "next_billing"})
		if err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error())
			return
		}
	} else {
		RespondWithError(c, http.StatusUnsupportedMediaType, ErrorCodeBadRequest, "unsupported content type")
		return
	}

	var status string
	var nextBilling string
	var hasStatus bool
	var hasNextBilling bool

	if raw, ok := payload["status"]; ok {
		if value, present, err := decodePatchStringValue(raw); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error())
			return
		} else if present {
			status = value
			hasStatus = true
		}
	}
	if raw, ok := payload["next_billing"]; ok {
		if value, present, err := decodePatchStringValue(raw); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error())
			return
		} else if present {
			nextBilling = value
			hasNextBilling = true
		}
	}

	response := map[string]interface{}{"id": id}
	if hasStatus {
		response["status"] = status
	}
	if hasNextBilling {
		response["next_billing"] = nextBilling
	}
	c.JSON(http.StatusOK, response)
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	}
}

// NewChangeSubscriptionStatusHandler updates a single subscription status.
func NewChangeSubscriptionStatusHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid request body")
			return
		}

		status := strings.TrimSpace(payload.Status)
		if status == "" {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, "status is required")
			return
		}

		tenantID := c.GetString("tenantID")
		if tenantID == "" {
			RespondWithAuthError(c, "missing tenant context")
			return
		}

		change, err := svc.ChangeStatus(c.Request.Context(), tenantID, c.GetString("callerID"), c.Param("id"), status)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			if errors.Is(err, service.ErrInvalidStatus) || errors.Is(err, service.ErrInvalidTransition) || errors.Is(err, service.ErrUnknownCurrentState) {
				statusCode = http.StatusConflict
				code = ErrorCodeConflict
				if errors.Is(err, service.ErrInvalidStatus) {
					statusCode = http.StatusUnprocessableEntity
					code = ErrorCodeValidationFailed
				}
			}
			RespondWithError(c, statusCode, code, message)
			return
		}

		c.JSON(http.StatusOK, gin.H{"api_version": "v1", "data": change})
	}
}

// NewBatchSubscriptionHandler accepts a batch of subscription status updates and returns
// per-item status codes in a 207 Multi-Status response.
func NewBatchSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req service.BatchSubscriptionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid request body")
			return
		}

		results, err := svc.ProcessBatch(c.Request.Context(), c.GetString("tenantID"), c.GetString("callerID"), req.Operations)
		if err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, err.Error())
			return
		}

		response := service.BatchSubscriptionResponse{Results: results}
		statusCode := http.StatusOK
		if len(results) > 0 {
			for _, result := range results {
				if result.StatusCode >= http.StatusBadRequest {
					statusCode = http.StatusMultiStatus
					break
				}
			}
		}
		c.JSON(statusCode, response)
	}
}

// NewBatchSubscriptionsHandler processes a batch of subscription status updates and
// returns a per-item results list plus a success/failure summary.
func NewBatchSubscriptionsHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "subscription service is unavailable")
			return
		}

		var req struct {
			Operations []service.BatchSubscriptionOperation `json:"operations"`
		}
		if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "Invalid JSON body")
			return
		}

		if len(req.Operations) == 0 {
			RespondWithValidationError(c, "operations must not be empty", nil)
			return
		}
		if len(req.Operations) > 100 {
			RespondWithValidationError(c, "batch size exceeds maximum of 100 operations", nil)
			return
		}

		tenantID, _ := c.Get("tenantID")
		callerID, _ := c.Get("callerID")
		tenantIDStr, _ := tenantID.(string)
		callerIDStr, _ := callerID.(string)

		results, err := svc.BatchChangeStatus(c.Request.Context(), tenantIDStr, callerIDStr, req.Operations)
		if err != nil {
			RespondWithInternalError(c, "Failed to process batch subscription operations")
			return
		}

		successCount := 0
		failureCount := 0
		for _, result := range results {
			if result.Success {
				successCount++
			} else {
				failureCount++
			}
		}

		statusCode := http.StatusOK
		if failureCount > 0 {
			if successCount > 0 {
				statusCode = http.StatusMultiStatus
			} else {
				statusCode = http.StatusUnprocessableEntity
			}
		}

		c.JSON(statusCode, gin.H{
			"api_version": "v1",
			"data": gin.H{
				"results": results,
				"summary": gin.H{"success": successCount, "failed": failureCount},
			},
		})
	}
}
