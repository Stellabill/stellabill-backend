package handlers

import (
	"net/http"

    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/reconciliation"
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

        snaps, err := adapter.FetchSnapshots(c.Request.Context())
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch snapshots"})
            return
        }

        snapMap := make(map[string]*reconciliation.Snapshot, len(snaps))
        for i := range snaps {
            s := snaps[i]
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

		snaps, err := adapter.FetchSnapshots(c.Request.Context())
		if err != nil {
			RespondWithInternalError(c, "Failed to fetch reconciliation snapshots")
			return
		}

		// Build snapshot map scoped to the caller's tenant for non-admin.
		snapMap := make(map[string]*reconciliation.Snapshot, len(snaps))
		for i := range snaps {
			s := snaps[i]
			if !isAdmin && s.TenantID != tid {
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

		matched := 0
		for _, r := range reports {
			if r.Matched {
				matched++
			}
		}

		if store != nil {
			if err := store.SaveReports(reports); err != nil {
				c.Header("X-Reconcile-Save-Error", err.Error())
			}
		}

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
			RespondWithValidationError(c, "Invalid pagination cursor", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}

		limitStr := c.Query("limit")
		limit, err := pagination.ParseLimit(limitStr, 20)
		if err != nil {
			RespondWithValidationError(c, "Invalid pagination limit", map[string]interface{}{
				"reason": err.Error(),
			})
			return
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
