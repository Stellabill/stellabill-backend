package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/validation"
)

// NewGetStatementHandler returns a gin.HandlerFunc that retrieves a full
// statement detail using the provided StatementService.
func NewGetStatementHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Read callerID from context (set by AuthMiddleware).
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		// 2. Validate :id path param.
		id := c.Param("id")
		id = validation.NormalizeString(id)
		if err := validation.ValidateUUID(id); err != nil {
			RespondWithValidationFields(c, "Invalid statement ID", validation.ValidateVar(id, "required,uuid"))
			return
		}

		// 3. Call service.
		detail, warnings, err := svc.GetDetail(c.Request.Context(), callerID.(string), id)
		if err != nil {
			statusCode, errorCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, errorCode, msg)
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
			RespondWithAuthError(c, "unauthorized")
			return
		}

		// 2. Build and validate query from optional filters.
		var reqQuery struct {
			SubscriptionID string `form:"subscription_id" validate:"omitempty,uuid"`
			Kind           string `form:"kind" validate:"omitempty,oneof=invoice credit_note payment"`
			Status         string `form:"status" validate:"omitempty,oneof=open paid void"`
			StartAfter     string `form:"start_after" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
			EndBefore      string `form:"end_before" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
			Page           int    `form:"page" validate:"omitempty,min=1"`
			PageSize       int    `form:"page_size" validate:"omitempty,min=1,max=100"`
		}

		if err := c.ShouldBindQuery(&reqQuery); err != nil {
			RespondWithValidationFields(c, "Invalid query parameters", []validation.FieldError{
				{Field: "query", Message: "Invalid query parameter format"},
			})
			return
		}

		if fieldErrors := validation.ValidateStruct(reqQuery); len(fieldErrors) > 0 {
			RespondWithValidationFields(c, "Validation failed", fieldErrors)
			return
		}

		q := repository.StatementQuery{
			SubscriptionID: reqQuery.SubscriptionID,
			Kind:           reqQuery.Kind,
			Status:         reqQuery.Status,
			StartAfter:     reqQuery.StartAfter,
			EndBefore:      reqQuery.EndBefore,
			Page:           reqQuery.Page,
			PageSize:       reqQuery.PageSize,
		}

		// 3. Call service.
		detail, count, warnings, err := svc.ListByCustomer(c.Request.Context(), callerID.(string), callerID.(string), q)
		if err != nil {
			statusCode, errorCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, errorCode, msg)
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
