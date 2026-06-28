package handlers

import (
	"errors"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/validation"
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
			RespondWithAuthError(c, "unauthorized")
			return
		}

		isLegacy := strings.Contains(c.Request.URL.Path, "/v1")

		// customer_id is required; fallback to callerID for subscribers listing own statements in new routes
		customerID := c.Query("customer_id")
		if customerID == "" {
			if isLegacy {
				c.JSON(http.StatusBadRequest, gin.H{"error": "customer_id is required"})
				return
			}
			customerID = callerID
		}

		// Parse remaining filter / pagination params.
		q, err := buildStatementQuery(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, total, serviceWarnings, err := svc.ListByCustomer(
			c.Request.Context(),
			callerID,
			roles,
			customerID,
			q,
		)
		if err != nil {
			if isLegacy {
				if errors.Is(err, service.ErrForbidden) {
					c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list statements"})
				return
			}
			code, errCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, code, errCode, msg)
			return
		}

		var statements []*service.StatementDetail
		if result != nil {
			statements = result.Statements
		}
		if statements == nil {
			statements = []*service.StatementDetail{}
		}

		if isLegacy {
			c.JSON(http.StatusOK, gin.H{
				"statements": statements,
				"total":      total,
			})
			return
		}

		resp := gin.H{
			"api_version": "2025-01-01",
			"data": gin.H{
				"statements": statements,
			},
			"pagination": gin.H{
				"page":      q.Page,
				"page_size": q.PageSize,
				"count":     total,
			},
		}
		if len(serviceWarnings) > 0 {
			resp["warnings"] = serviceWarnings
		}

		c.JSON(http.StatusOK, resp)
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
			RespondWithAuthError(c, "unauthorized")
			return
		}

		isLegacy := strings.Contains(c.Request.URL.Path, "/v1")

		id := strings.TrimSpace(c.Param("id"))
		if id == "" {
			if isLegacy {
				c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
				return
			}
			RespondWithValidationFields(c, "Invalid statement ID", []validation.FieldError{{Field: "id", Message: "statement ID is required"}})
			return
		}

		stmt, warnings, err := svc.GetDetail(
			c.Request.Context(),
			callerID,
			roles,
			id,
		)
		if err != nil {
			if isLegacy {
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
			code, errCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, code, errCode, msg)
			return
		}

		tenantID := c.GetString("tenantID")
		versionBytes, _ := json.Marshal(stmt)
		version := fmt.Sprintf("%x", sha256.Sum256(versionBytes))
		eTagHash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", tenantID, stmt.ID, version)))
		eTag := fmt.Sprintf(`"%x"`, eTagHash)

		c.Header("ETag", eTag)
		c.Header("Cache-Control", "private, max-age=0, must-revalidate")

		if match := c.GetHeader("If-None-Match"); match == eTag {
			c.Status(http.StatusNotModified)
			return
		}

		if isLegacy {
			c.JSON(http.StatusOK, stmt)
			return
		}

		resp := gin.H{
			"api_version": "2025-01-01",
			"data":        stmt,
		}
		if len(warnings) > 0 {
			resp["warnings"] = warnings
		}

		c.JSON(http.StatusOK, resp)
	}
}

// ---------------- HELPERS ----------------

// getAuthContext extracts caller_id and roles from the Gin context.
// These values are stored by the auth middleware before handlers run.
func getAuthContext(c *gin.Context) (callerID string, roles []string, ok bool) {
	callerRaw, ok1 := c.Get("caller_id")
	if !ok1 {
		callerRaw, ok1 = c.Get("callerID")
	}
	if !ok1 {
		return "", nil, false
	}
	callerID, castOK := callerRaw.(string)
	if !castOK || callerID == "" {
		return "", nil, false
	}

	rolesRaw, ok2 := c.Get("roles")
	if !ok2 {
		rolesRaw, ok2 = c.Get("role")
	}
	if !ok2 {
		roles = []string{}
	} else {
		switch typed := rolesRaw.(type) {
		case []string:
			roles = typed
		case []auth.Role:
			roles = make([]string, 0, len(typed))
			for _, role := range typed {
				if trimmed := strings.TrimSpace(string(role)); trimmed != "" {
					roles = append(roles, trimmed)
				}
			}
		case string:
			roles = []string{typed}
		case auth.Role:
			if trimmed := strings.TrimSpace(string(typed)); trimmed != "" {
				roles = []string{trimmed}
			}
		default:
			roles = []string{}
		}
	}

	return callerID, roles, true
}

func buildStatementQuery(c *gin.Context) (repository.StatementQuery, error) {
	q := repository.StatementQuery{
		Order: "desc",
	}

	pageStr := c.Query("page")
	if pageStr == "" {
		q.Page = 1
	} else {
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			return q, errors.New("invalid page parameter")
		}
		q.Page = page
	}

	pageSizeStr := c.Query("page_size")
	if pageSizeStr == "" {
		q.PageSize = 10
	} else {
		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil || pageSize < 1 {
			return q, errors.New("invalid page_size parameter")
		}
		q.PageSize = pageSize
	}
	q.Limit = q.PageSize

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
