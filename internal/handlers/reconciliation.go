package handlers

import (
	"net/http"
	"strconv"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/reconciliation"

	"github.com/gin-gonic/gin"
)

// NewReconcileHandler returns a handler that accepts a list of backend subscriptions
// (JSON array) and compares them against snapshots fetched from the provided Adapter.
// If a non-nil store is provided, reports will be persisted.
// Request body: [{subscription_id,...}, ...]
func NewReconcileHandler(adapter reconciliation.Adapter, store reconciliation.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var backendSubs []reconciliation.BackendSubscription
		if err := c.ShouldBindJSON(&backendSubs); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		roleVal, _ := c.Get(auth.RoleContextKey)
		var roleStr string
		if r, ok := roleVal.(auth.Role); ok {
			roleStr = string(r)
		} else if s, ok := roleVal.(string); ok {
			roleStr = s
		}
		tenantID := c.GetString("tenantID")

		if roleStr != string(auth.RoleAdmin) && tenantID == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "tenant context missing"})
			return
		}

		for i := range backendSubs {
			if roleStr != string(auth.RoleAdmin) {
				if backendSubs[i].TenantID != "" && backendSubs[i].TenantID != tenantID {
					c.JSON(http.StatusForbidden, gin.H{"error": "cross-tenant reconciliation forbidden"})
					return
				}
				backendSubs[i].TenantID = tenantID
			} else {
				if backendSubs[i].TenantID == "" {
					backendSubs[i].TenantID = tenantID
				}
			}
		}

		snaps, err := adapter.FetchSnapshots(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch snapshots"})
			return
		}

		snapMap := make(map[string]*reconciliation.Snapshot)
		for i := range snaps {
			s := snaps[i]
			if roleStr != string(auth.RoleAdmin) && s.TenantID != tenantID {
				continue
			}
			snapMap[s.SubscriptionID] = &s
		}

		reconciler := reconciliation.New()
		reports := make([]reconciliation.Report, 0, len(backendSubs))
		for _, b := range backendSubs {
			rep := reconciler.Compare(b, snapMap[b.SubscriptionID])
			reports = append(reports, rep)
		}

		// summary
		matched := 0
		for _, r := range reports {
			if r.Matched {
				matched++
			}
		}

		// persist if store configured
		if store != nil {
			// best-effort save; don't fail the request on save error but log via header
			if err := store.SaveReports(reports); err != nil {
				c.Header("X-Reconcile-Save-Error", err.Error())
			}
		}

		// Audit log the reconciliation action
		outcome := "success"
		if matched < len(reports) {
			outcome = "partial"
		}
		audit.LogAction(c, "reconciliation.execute", "reconciliation", outcome, map[string]string{
			"total":      strconv.Itoa(len(reports)),
			"matched":    strconv.Itoa(matched),
			"mismatched": strconv.Itoa(len(reports) - matched),
			"tenant_id":  tenantID,
		})

		c.JSON(http.StatusOK, gin.H{
			"summary": gin.H{"total": len(reports), "matched": matched, "mismatched": len(reports) - matched},
			"reports": reports,
		})
	}
}

// NewListReportsHandler returns a handler that lists reconciliation reports.
// Admin sees all reports; merchants see only their tenant's reports.
// Supports cursor-based pagination with tenant-scoped cursors.
func NewListReportsHandler(store reconciliation.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		tenantID, exists := c.Get("tenantID")
		if !exists {
			RespondWithAuthError(c, "Missing tenant context")
			return
		}
		tid := tenantID.(string)

		roles := auth.ExtractRoles(c)
		if !hasAnyPermission(roles, auth.PermReadReconciliation) {
			RespondWithError(c, http.StatusForbidden, ErrorCodeForbidden, "Insufficient permissions to view reports")
			return
		}

		isAdmin := hasRole(roles, auth.RoleAdmin)

		// Validate scoped cursor
		cursorStr := c.Query("cursor")
		cursor, err := pagination.DecodeScopedCursor(cursorStr, tid)
		if err != nil {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Invalid pagination cursor", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}

		limitStr := c.Query("limit")
		limit, err := pagination.ParseLimit(limitStr, 20)
		if err != nil {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Invalid pagination limit", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}

		// Domain-specific hard cap for reconciliation reports: do not allow
		// more than 20 items per page regardless of the global MaxLimit.
		if limit > 20 {
			limit = 20
		}

		var reports []reconciliation.Report
		if isAdmin {
			reports, err = store.ListReports()
		} else {
			reports, err = store.ListReportsByTenant(tid)
		}
		if err != nil {
			RespondWithInternalError(c, "Failed to load reports")
			return
		}

		page := pagination.PaginateSlice(reports, cursor, limit)

		// Re-encode the next cursor with tenant scope.
		nextCursor := ""
		if page.HasMore && len(page.Items) > 0 {
			last := page.Items[len(page.Items)-1]
			nextCursor = pagination.EncodeScopedCursor(last.GetID(), last.GetSortValue(), tid)
		}

		c.JSON(http.StatusOK, gin.H{
			"reports":     page.Items,
			"next_cursor": nextCursor,
			"has_more":    page.HasMore,
		})
	}
}

func hasAnyPermission(roles []auth.Role, perm auth.Permission) bool {
	for _, r := range roles {
		if auth.HasPermission(r, perm) {
			return true
		}
	}
	return false
}

func hasRole(roles []auth.Role, target auth.Role) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}
