package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// NewGetStatementHandler returns a gin.HandlerFunc that retrieves a full
// statement detail using the provided StatementService.
func NewGetStatementHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Read callerID from context (set by AuthMiddleware).
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		// 2. Validate :id path param.
		id := c.Param("id")
		if strings.TrimSpace(id) == "" {
			RespondWithValidationError(c, "statement id is required", map[string]interface{}{
				"field":  "id",
				"reason": "cannot be empty",
			})
			return
		}

		// 3. Call service.
		detail, warnings, err := svc.GetDetail(c.Request.Context(), callerID.(string), id)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, code, message)
			return
		}

		// 4. Build response envelope.
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
		// 1. Read callerID from context (set by AuthMiddleware).
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		// 2. Build query from optional filters.
		q := repository.StatementQuery{
			SubscriptionID: c.Query("subscription_id"),
			Kind:           c.Query("kind"),
			Status:         c.Query("status"),
			StartAfter:     c.Query("start_after"),
			EndBefore:      c.Query("end_before"),
		}

		if ps := c.Query("page_size"); ps != "" {
			if v, err := strconv.Atoi(ps); err == nil {
				q.PageSize = v
			}
		}
		if p := c.Query("page"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				q.Page = v
			}
		}

		// 3. Call service.
		detail, count, warnings, err := svc.ListByCustomer(c.Request.Context(), callerID.(string), callerID.(string), q)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, code, message)
			return
		}

		// 4. Normalise pagination values for response.
		page := q.Page
		if page <= 0 {
			page = 1
		}
		pageSize := q.PageSize
		if pageSize <= 0 {
			pageSize = 10
		}

		// 5. Build response envelope with pagination.
		resp := service.ResponseEnvelopeWithPagination{
			ResponseEnvelope: service.ResponseEnvelope{
				APIVersion: "2025-01-01",
				Data:       detail,
				Warnings:   warnings,
			},
			Pagination: service.PaginationMetadata{
				Page:     page,
				PageSize: pageSize,
				Count:    count,
			},
		}

		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, resp)
	}
}
