package handlers

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "stellarbill-backend/internal/auth"
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

        c.JSON(http.StatusOK, gin.H{
            "summary": gin.H{"total": len(reports), "matched": matched, "mismatched": len(reports) - matched},
            "reports": reports,
        })
    }
}
