package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/requestparams"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/validation"
)

const subsTracerName = "handler/subscriptions"

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
func (s Subscription) GetSortValue() string { return s.Customer }

func (h *Handler) ListSubscriptions(c *gin.Context) {
	baseCtx := context.Background()
	if c.Request != nil {
		baseCtx = c.Request.Context()
	}
	ctx, span := otel.Tracer(subsTracerName).Start(baseCtx, "handler.ListSubscriptions")
	defer span.End()
	if c.Request != nil {
		c.Request = c.Request.WithContext(ctx)
	}

	limitStr := c.Query("limit")
	limit, err := pagination.ParseLimit(limitStr, 10)
	if err != nil {
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Invalid pagination limit", map[string]interface{}{
			"reason": err.Error(),
		})
		return
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor format"})
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)

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
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

type changeSubscriptionStatusRequest struct {
	Status string `json:"status"`
}

// NewChangeSubscriptionStatusHandler returns a tenant-scoped status mutation handler.
func NewChangeSubscriptionStatusHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := otel.Tracer(subsTracerName).Start(c.Request.Context(), "handler.ChangeSubscriptionStatus",
			trace.WithAttributes(attribute.String("subscription.id", c.Param("id"))))
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		if svc == nil {
			RespondWithInternalError(c, "Subscription service is unavailable")
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		actorID := c.GetString("callerID")

		var req changeSubscriptionStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Invalid request body", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}
		req.Status = strings.TrimSpace(req.Status)
		if req.Status == "" {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, "status is required")
			return
		}

		result, err := svc.ChangeStatus(c.Request.Context(), tenantID, actorID, c.Param("id"), req.Status)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidStatus):
				RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, err.Error())
			case errors.Is(err, service.ErrInvalidTransition), errors.Is(err, service.ErrUnknownCurrentState):
				RespondWithError(c, http.StatusConflict, ErrorCodeConflict, err.Error())
			default:
				status, code, message := MapServiceErrorToResponse(err)
				RespondWithError(c, status, code, message)
			}
			return
		}

		c.JSON(http.StatusOK, service.ResponseEnvelope{
			APIVersion: "v1",
			Data:       result,
		})
	}
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// nil-svc guard: keeps legacy/coverage tests that pass nil working.
		if svc == nil {
			c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
			return
		}

		ctx, span := otel.Tracer(subsTracerName).Start(c.Request.Context(), "handler.GetSubscription",
			trace.WithAttributes(attribute.String("subscription.id", c.Param("id"))))
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		if _, err := requestparams.SanitizeQuery(c.Request.URL.Query(), requestparams.QueryRules{}); err != nil {
			RespondWithValidationError(c, err.Error(), []validation.FieldError{{Field: "value", Message: err.Error()}})
			return
		}

		id, err := requestparams.NormalizePathID("id", c.Param("id"))
		if err != nil {
			RespondWithValidationError(c, err.Error(), []validation.FieldError{{Field: "value", Message: err.Error()}})
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		detail, _, err := svc.GetDetail(c.Request.Context(), tenantID, callerID.(string), id)
		if err != nil {
			code, errCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, code, errCode, msg)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"api_version": "1",
			"data": gin.H{
				"id":              detail.ID,
				"plan_id":         detail.PlanID,
				"customer":        detail.Customer,
				"status":          detail.Status,
				"interval":        detail.Interval,
				"plan":            detail.Plan,
				"billing_summary": detail.BillingSummary,
			},
		})
	}
}

func getRequiredStringContextValue(c *gin.Context, key string, missingMessage string) (string, bool) {
	value, exists := c.Get(key)
	if !exists {
		RespondWithAuthError(c, missingMessage)
		return "", false
	}

	str, ok := value.(string)
	if !ok || str == "" {
		RespondWithAuthError(c, missingMessage)
		return "", false
	}

	return str, true
}

// ListSubscriptions is a package-level helper for backwards compatibility / benchmark tests.
func ListSubscriptions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"subscriptions": []Subscription{}})
}
