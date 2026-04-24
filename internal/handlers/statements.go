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
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// 2. Validate :id path param.
		id := c.Param("id")
		if strings.TrimSpace(id) == "" {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusBadRequest, gin.H{"error": "statement id required"})
			return
		}

		// 3. Call service.
		detail, warnings, err := svc.GetDetail(c.Request.Context(), callerID.(string), id)
		if err != nil {
			c.Header("Content-Type", "application/json; charset=utf-8")
			switch err {
			case service.ErrNotFound:
				c.JSON(http.StatusNotFound, gin.H{"error": "statement not found"})
			case service.ErrDeleted:
				c.JSON(http.StatusGone, gin.H{"error": "statement has been deleted"})
			case service.ErrForbidden:
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
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
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
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

		limitStr := c.DefaultQuery("limit", "10")
		limit, _ := strconv.Atoi(limitStr)
		if limit <= 0 {
			limit = 10
		}
		q.PageSize = limit // Reuse PageSize as Limit for now in repo

		cursorStr := c.Query("cursor")
		q.StartAfter = cursorStr // Standardize on StartAfter as the cursor field

		// 3. Call service.
		detail, count, warnings, err := svc.ListByCustomer(c.Request.Context(), callerID.(string), callerID.(string), q)
		if err != nil {
			c.Header("Content-Type", "application/json; charset=utf-8")
			switch err {
			case service.ErrForbidden:
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		// 4. Build response envelope with cursor pagination.
		// Since we don't have a real cursor implementation in the service yet, we'll simulate.
		hasMore := count > limit
		nextCursor := ""
		if hasMore && len(detail.Statements) > 0 {
			nextCursor = detail.Statements[len(detail.Statements)-1].ID
		}

		resp := service.ResponseEnvelopeWithPagination{
			ResponseEnvelope: service.ResponseEnvelope{
				APIVersion: "2025-01-01",
				Data:       detail,
				Warnings:   warnings,
			},
			Pagination: service.PaginationMetadata{
				NextCursor: nextCursor,
				Limit:      limit,
				HasMore:    hasMore,
			},
		}

		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, resp)
	}
}

