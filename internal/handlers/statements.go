package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"stellarbill-backend/internal/auth"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// NewGetStatementHandler returns a gin.HandlerFunc that retrieves a full
// statement detail using the provided StatementService.
func NewGetStatementHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		roles := auth.ExtractRoles(c)

		// Validate :id path param.
		id := c.Param("id")
		if strings.TrimSpace(id) == "" {
			RespondWithValidationError(c, "statement id is required", map[string]interface{}{
				"field":  "id",
				"reason": "cannot be empty",
			})
			return
		}

		// Call service.
		detail, warnings, err := svc.GetDetail(c.Request.Context(), callerID.(string), rolesToStrings(roles), id)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, code, message)
			return
		}

		// Build response envelope.
		resp := service.ResponseEnvelope{
			APIVersion: "2025-01-01",
			Data:       detail,
			Warnings:   warnings,
		}

		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, resp)
	}
}

// NewListStatementsHandler returns a gin.HandlerFunc that lists billing
// statements for a customer using the provided StatementService.
func NewListStatementsHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		roles := auth.ExtractRoles(c)

		// Build query from optional filters.
		q := repository.StatementQuery{
			SubscriptionID: c.Query("subscription_id"),
			Kind:           c.Query("kind"),
			Status:         c.Query("status"),
			Order:          c.Query("order"),
		}

		limitStr := c.DefaultQuery("limit", "10")
		limit, _ := strconv.Atoi(limitStr)
		if limit <= 0 || limit > 100 {
			limit = 10
		}
		q.Limit = limit

		// Cursor pagination parameters
		q.StartingAfter = c.Query("starting_after")
		q.EndingBefore = c.Query("ending_before")

		// Call service.
		customerID := c.Query("customer_id")
		if customerID == "" {
			customerID = callerID.(string)
		}

		detail, count, warnings, err := svc.ListByCustomer(c.Request.Context(), callerID.(string), rolesToStrings(roles), customerID, q)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, code, message)
			return
		}

		// Build response envelope with cursor pagination.
		// Deterministic next/prev cursors based on the returned slice.
		hasMore := len(detail.Statements) >= limit
		nextCursor := ""
		prevCursor := ""
		
		if len(detail.Statements) > 0 {
			nextCursor = detail.Statements[len(detail.Statements)-1].ID
			prevCursor = detail.Statements[0].ID
		}

		resp := service.ResponseEnvelopeWithPagination{
			ResponseEnvelope: service.ResponseEnvelope{
				APIVersion: "2025-01-01",
				Data:       detail,
				Warnings:   warnings,
			},
			Pagination: service.PaginationMetadata{
				TotalCount: count,
				HasMore:    hasMore,
				NextCursor: nextCursor,
				Limit:      limit,
			},
		}

		// Custom header for backward pagination if needed
		if prevCursor != "" {
			c.Header("X-Prev-Cursor", prevCursor)
		}

		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, resp)
	}
}

func rolesToStrings(roles []auth.Role) []string {
	strs := make([]string, len(roles))
	for i, r := range roles {
		strs[i] = string(r)
	}
	return strs
}
