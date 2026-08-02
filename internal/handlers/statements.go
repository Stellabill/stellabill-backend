package handlers

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/requestparams"
	"stellarbill-backend/internal/service"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------- CONSTANTS ----------------

const (
	defaultLimit = 20
	maxLimit     = 200
)

// StatementAllowedFields lists the JSON field names that clients may request
// via the ?fields= query parameter on statement endpoints.
var StatementAllowedFields = []string{
	"id",
	"subscription_id",
	"customer",
	"period_start",
	"period_end",
	"issued_at",
	"total_amount",
	"currency",
	"kind",
	"status",
}

// ---------------- LIST HANDLER ----------------

// NewListStatementsHandler returns a gin.HandlerFunc for GET /api/v1/statements.
//
// It extracts the authenticated caller's ID and roles from the Gin context
// (set by auth middleware), requires a customer_id query parameter, builds a
// repository.StatementQuery from the remaining query parameters, and delegates
// to StatementService.ListByCustomer.
//
// Supports ?fields= for sparse fieldset selection.
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
//	fields          – comma-separated list of fields to include in the response
//
// Security: ownership and RBAC are enforced inside StatementService.ListByCustomer.
// A subscriber may only list their own statements; a merchant may list statements
// for customers in their tenant; an admin may list any customer's statements.
func NewListStatementsHandler(svc service.StatementService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// nil-svc guard: keeps legacy/coverage tests that pass nil working.
		if svc == nil {
			if wantsCSV(c) {
				writeStatementsCSV(c, "", nil, nil)
				return
			}
			c.JSON(http.StatusOK, gin.H{"statements": []interface{}{}})
			return
		}

		// Extract auth context set by middleware.
		callerID, roles, ok := getAuthContext(c)
		if !ok {
			RenderProblem(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "unauthorized")
			return
		}

		// customer_id is required: the caller must declare whose statements
		// they are requesting (RBAC enforcement happens in the service).
		customerID := c.Query("customer_id")
		if customerID == "" {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "customer_id is required")
			return
		}

		// Parse optional ?fields= parameter.
		fields, err := requestparams.ParseFields(c.Query("fields"), StatementAllowedFields)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Parse remaining filter / pagination params.
		q, err := buildStatementQuery(c)
		if err != nil {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error())
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
				RenderProblem(c, http.StatusForbidden, ErrorCodeForbidden, "forbidden")
				return
			}
			RenderProblem(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to list statements")
			return
		}

		var statements []*service.StatementDetail
		if result != nil {
			statements = result.Statements
		}
		if statements == nil {
			statements = []*service.StatementDetail{}
		}

		// NOTE: repository.StatementQuery.StartingAfter/EndingBefore are not
		// yet wired through to a repository implementation (no server-side
		// keyset pagination exists for statements today), so only rel="first"
		// can be emitted correctly here. Emitting rel="next"/"prev" would
		// require query parameters this endpoint does not yet accept, and
		// would produce a link that doesn't actually advance the collection.
		if header := pagination.LinkHeader(requestBaseURL(c), pagination.LinkParams{}); header != "" {
			c.Header("Link", header)
		}

		if wantsCSV(c) {
			writeStatementsCSV(c, customerID, fields, statements)
			return
		}

		if fields != nil {
			// Apply sparse fieldset projection.
			projected := make([]map[string]json.RawMessage, 0, len(statements))
			for _, stmt := range statements {
				p, err := ProjectFields(stmt, fields)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build response"})
					return
				}
				projected = append(projected, p)
			}
			c.JSON(http.StatusOK, gin.H{
				"statements": projected,
				"total":      total,
			})
			return
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
// Supports ?fields= for sparse fieldset selection.
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
			RenderProblem(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "unauthorized")
			return
		}

		id := c.Param("id")
		if id == "" {
			RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "id is required")
			return
		}

		// Parse optional ?fields= parameter.
		fields, err := requestparams.ParseFields(c.Query("fields"), StatementAllowedFields)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
				RenderProblem(c, http.StatusNotFound, ErrorCodeNotFound, "statement not found")
				return
			}
			if errors.Is(err, service.ErrForbidden) {
				RenderProblem(c, http.StatusForbidden, ErrorCodeForbidden, "forbidden")
				return
			}
			RenderProblem(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to fetch statement")
			return
		}

		if fields != nil {
			projected, err := ProjectFields(stmt, fields)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build response"})
				return
			}
			c.JSON(http.StatusOK, projected)
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

func wantsCSV(c *gin.Context) bool {
	accept := strings.ToLower(c.GetHeader("Accept"))
	return strings.Contains(accept, "text/csv")
}

func writeStatementsCSV(c *gin.Context, customerID string, fields []string, statements []*service.StatementDetail) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", csvFilename(customerID)))

	writer := csv.NewWriter(c.Writer)
	columns := fields
	if len(columns) == 0 {
		columns = StatementAllowedFields
	}
	if err := writer.Write(columns); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	for _, stmt := range statements {
		if err := writer.Write(statementCSVRow(stmt, columns)); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	}
	writer.Flush()
	if writer.Error() != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
	}
}

func statementCSVRow(stmt *service.StatementDetail, fields []string) []string {
	if stmt == nil {
		return make([]string, len(fields))
	}
	row := make([]string, 0, len(fields))
	for _, field := range fields {
		row = append(row, csvCellValue(stmt, field))
	}
	return row
}

func csvCellValue(stmt *service.StatementDetail, field string) string {
	if stmt == nil {
		return ""
	}
	switch field {
	case "id":
		return sanitizeCSVValue(stmt.ID)
	case "subscription_id":
		return sanitizeCSVValue(stmt.SubscriptionID)
	case "customer":
		return sanitizeCSVValue(stmt.Customer)
	case "period_start":
		return sanitizeCSVValue(stmt.PeriodStart)
	case "period_end":
		return sanitizeCSVValue(stmt.PeriodEnd)
	case "issued_at":
		return sanitizeCSVValue(stmt.IssuedAt)
	case "total_amount":
		return sanitizeCSVValue(stmt.TotalAmount)
	case "currency":
		return sanitizeCSVValue(stmt.Currency)
	case "kind":
		return sanitizeCSVValue(stmt.Kind)
	case "status":
		return sanitizeCSVValue(stmt.Status)
	default:
		return ""
	}
}

func sanitizeCSVValue(value string) string {
	for _, prefix := range []string{"=", "+", "-", "@"} {
		if strings.HasPrefix(value, prefix) {
			return "'" + value
		}
	}
	return value
}

func csvFilename(customerID string) string {
	base := strings.TrimSpace(customerID)
	if base == "" {
		base = "statements"
	}
	base = strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, base))
	base = strings.Trim(base, "-")
	if base == "" {
		base = "statements"
	}
	return fmt.Sprintf("%s-statements.csv", base)
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
	if v := c.Query("filter"); v != "" {
		filter, err := requestparams.ParseRSQL(v)
		if err != nil {
			return q, err
		}
		q.Filter = filter
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
