package handlers

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "stellabill-backend/internal/reconciliation"
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

        c.JSON(http.StatusOK, gin.H{
            "summary": gin.H{"total": len(reports), "matched": matched, "mismatched": len(reports) - matched},
            "reports": reports,
        })
    }
}
