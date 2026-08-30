package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"stellarbill-backend/internal/service"
)

// maxBatchOperations caps the number of status changes allowed in a single
// batch request.
const maxBatchOperations = 100

// batchStatusOperation is the per-operation payload for NewBatchSubscriptionHandler.
type batchStatusOperation struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key"`
}

// batchStatusResult is the per-operation outcome for NewBatchSubscriptionHandler.
type batchStatusResult struct {
	Index      int    `json:"index"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
}

// applyBatchStatus runs a single ChangeStatus and maps the outcome back to an
// HTTP status code and client-facing message.
func applyBatchStatus(c *gin.Context, svc service.SubscriptionService, tenantID, callerID, subscriptionID, targetStatus string) (int, string) {
	ctx, span := tracer.Start(c.Request.Context(), "handler.BatchSubscriptionStatus")
	defer span.End()

	span.SetAttributes(
		attribute.String("subscription.id", subscriptionID),
		attribute.String("tenant.id", tenantID),
		attribute.String("target.status", targetStatus),
	)

	if svc == nil {
		span.SetStatus(codes.Error, "subscription service unavailable")
		return http.StatusServiceUnavailable, "subscription service unavailable"
	}

	_, err := svc.ChangeStatus(ctx, tenantID, callerID, subscriptionID, targetStatus)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			return http.StatusNotFound, "subscription not found"
		case errors.Is(err, service.ErrDeleted):
			return http.StatusGone, "subscription deleted"
		case errors.Is(err, service.ErrInvalidStatus),
			errors.Is(err, service.ErrInvalidTransition),
			errors.Is(err, service.ErrUnknownCurrentState):
			return http.StatusConflict, err.Error()
		default:
			return http.StatusInternalServerError, "failed to change subscription status"
		}
	}
	return http.StatusOK, ""
}

// NewBatchSubscriptionHandler returns a gin.HandlerFunc that applies multiple
// subscription status changes and reports a per-operation outcome using HTTP
// Multi-Status (207). An operation without an idempotency_key is rejected
// inline without touching the service layer.
func NewBatchSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, _ := c.Get("tenantID")
		tenantIDStr, _ := tenantID.(string)
		callerID, _ := c.Get("callerID")
		callerIDStr, _ := callerID.(string)

		var payload struct {
			Operations []batchStatusOperation `json:"operations"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid batch payload")
			return
		}
		if len(payload.Operations) == 0 {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "operations are required")
			return
		}

		results := make([]batchStatusResult, 0, len(payload.Operations))
		for i, op := range payload.Operations {
			res := batchStatusResult{Index: i}
			switch {
			case op.IdempotencyKey == "":
				res.StatusCode = http.StatusBadRequest
				res.Message = "idempotency_key is required"
			case op.SubscriptionID == "":
				res.StatusCode = http.StatusBadRequest
				res.Message = "subscription_id is required"
			case op.Status == "":
				res.StatusCode = http.StatusBadRequest
				res.Message = "status is required"
			default:
				res.StatusCode, res.Message = applyBatchStatus(c, svc, tenantIDStr, callerIDStr, op.SubscriptionID, op.Status)
			}
			results = append(results, res)
		}

		c.JSON(http.StatusMultiStatus, gin.H{"results": results})
	}
}

// batchStatusOperationV2 is the per-operation payload for NewBatchSubscriptionsHandler.
type batchStatusOperationV2 struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key"`
}

// batchStatusResultV2 is the per-operation outcome for NewBatchSubscriptionsHandler.
type batchStatusResultV2 struct {
	Index      int    `json:"index"`
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
}

// batchStatusSummary groups operation outcomes for NewBatchSubscriptionsHandler.
type batchStatusSummary struct {
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

// NewBatchSubscriptionsHandler returns a gin.HandlerFunc that applies a batch
// of subscription status changes. The request is validated whole before any
// operation runs: more than maxBatchOperations yields 400, and every operation
// must carry an idempotency_key (422 otherwise). Per-operation results and a
// success/failed summary are returned with HTTP Multi-Status (207).
func NewBatchSubscriptionsHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, _ := c.Get("tenantID")
		tenantIDStr, _ := tenantID.(string)
		callerID, _ := c.Get("callerID")
		callerIDStr, _ := callerID.(string)

		var payload struct {
			Operations []batchStatusOperationV2 `json:"operations"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid batch payload")
			return
		}
		if len(payload.Operations) == 0 || len(payload.Operations) > maxBatchOperations {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "operations must contain between 1 and 100 items")
			return
		}
		for _, op := range payload.Operations {
			if op.IdempotencyKey == "" {
				RenderProblem(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, "idempotency_key is required for every operation")
				return
			}
		}

		results := make([]batchStatusResultV2, 0, len(payload.Operations))
		summary := batchStatusSummary{}
		for i, op := range payload.Operations {
			res := batchStatusResultV2{Index: i, Status: op.Status}
			res.StatusCode, res.Message = applyBatchStatus(c, svc, tenantIDStr, callerIDStr, op.ID, op.Status)
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				summary.Success++
			} else {
				summary.Failed++
			}
			results = append(results, res)
		}

		c.JSON(http.StatusMultiStatus, gin.H{
			"data": gin.H{
				"results": results,
				"summary": summary,
			},
		})
	}
}
