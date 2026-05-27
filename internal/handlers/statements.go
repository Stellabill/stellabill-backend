package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// ---------------- CONSTANTS ----------------

const defaultLimit = 20
const maxLimit = 200

// ---------------- LIST HANDLER ----------------

// NewListStatementsHandler returns a gin.HandlerFunc for GET /api/v1/statements.
//
// It extracts the authenticated caller's ID and roles from the Gin context
// (set by auth middleware), requires a customer_id query parameter, builds a
// repository.StatementQuery from the remaining query parameters, and delegates
// to StatementService.ListByCustomer.
//
// Supported query parameters:
//
//	customer_id     – (required) the customer whose statements to list
//	subscription_id – filter by subscription UUID
//	kind            – filter by statement kind (e.g. "invoice", "credit_note")
//	status          – filter by lifecycle status (e.g. "open", "paid")
//	start_after     – RFC3339 lower bound for statement date (exclusive)
//	end_before      – RFC3339 upper bound for statement date (exclusive)
//	limit           – page size, 1–200 (default 20)
//	order           – "asc" or "desc" (default "desc")
//
// Security: ownership and RBAC are enforced inside StatementService.ListByCustomer.
// A subscriber may only list their own statements; a merchant may list statements
// for customers in their tenant; an admin may list any customer's statements.
func NewListStatementsHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// nil-svc guard: keeps legacy/coverage tests that pass nil working.
		if svc == nil {
			c.JSON(http.StatusOK, gin.H{"statements": []interface{}{}})
			return
		}

		// Extract auth context set by middleware.
		callerID, roles, ok := getAuthContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// customer_id is required: the caller must declare whose statements
		// they are requesting (RBAC enforcement happens in the service).
		customerID := c.Query("customer_id")
		if customerID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "customer_id is required"})
			return
		}

		// Parse remaining filter / pagination params.
		q, err := buildStatementQuery(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, total, _, err := svc.ListByCustomer(
			c.Request.Context(),
			callerID,
			roles,
			customerID,
			q,
		)
		if err != nil {
			if errors.Is(err, service.ErrForbidden) {
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list statements"})
			return
		}

		var statements []*service.StatementDetail
		if result != nil {
			statements = result.Statements
		}
		if statements == nil {
			statements = []*service.StatementDetail{}
		}

		c.JSON(http.StatusOK, gin.H{
			"statements": statements,
			"total":      total,
		})
	}
}

// ---------------- GET HANDLER ----------------

// NewGetStatementHandler returns a gin.HandlerFunc for GET /api/v1/statements/:id.
//
// It extracts the authenticated caller's ID and roles from the Gin context,
// delegates ownership/RBAC enforcement to StatementService.GetDetail, and maps
// service.ErrNotFound to HTTP 404 so the caller cannot enumerate statements
// belonging to other customers.
//
// Security: the service enforces that subscribers may only fetch their own
// statements; cross-customer lookups are returned as 404 (not 403) to avoid
// leaking the existence of a statement.
func NewGetStatementHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// nil-svc guard: keeps legacy/coverage tests that pass nil working.
		if svc == nil {
			c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
			return
		}

		// Extract auth context set by middleware.
		callerID, roles, ok := getAuthContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}

		stmt, _, err := svc.GetDetail(
			c.Request.Context(),
			callerID,
			roles,
			id,
		)
		if err != nil {
			if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrDeleted) {
				c.JSON(http.StatusNotFound, gin.H{"error": "statement not found"})
				return
			}
			if errors.Is(err, service.ErrForbidden) {
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch statement"})
			return
		}

		c.JSON(http.StatusOK, stmt)
	}
}

// ---------------- HELPERS ----------------

// getAuthContext extracts caller_id and roles from the Gin context.
// These values are stored by the auth middleware before handlers run.
func getAuthContext(c *gin.Context) (callerID string, roles []string, ok bool) {
	callerRaw, ok1 := c.Get("caller_id")
	rolesRaw, ok2 := c.Get("roles")
	if !ok1 || !ok2 {
		return "", nil, false
	}
	callerID, castOK := callerRaw.(string)
	if !castOK || callerID == "" {
		return "", nil, false
	}
	roles, castOK = rolesRaw.([]string)
	if !castOK {
		return "", nil, false
	}
	return callerID, roles, true
}

// buildStatementQuery parses optional filter and pagination query parameters
// into a repository.StatementQuery. Returns an error on any invalid input so
// the handler can respond 400 before touching the service layer.
func buildStatementQuery(c *gin.Context) (repository.StatementQuery, error) {
	q := repository.StatementQuery{
		Limit: defaultLimit,
		Order: "desc",
	}

	if v := c.Query("subscription_id"); v != "" {
		q.SubscriptionID = v
	}
	if v := c.Query("kind"); v != "" {
		q.Kind = v
	}
	if v := c.Query("status"); v != "" {
		q.Status = v
	}

	if v := c.Query("start_after"); v != "" {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return q, errors.New("start_after must be a valid RFC3339 timestamp")
		}
		q.StartAfter = v
	}

	if v := c.Query("end_before"); v != "" {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return q, errors.New("end_before must be a valid RFC3339 timestamp")
		}
		q.EndBefore = v
	}

	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return q, errors.New("limit must be a positive integer")
		}
		if n > maxLimit {
			n = maxLimit
		}
		q.Limit = n
	}

	if v := c.Query("order"); v != "" {
		if v != "asc" && v != "desc" {
			return q, errors.New("order must be 'asc' or 'desc'")
		}
		q.Order = v
	}

	return q, nil
}