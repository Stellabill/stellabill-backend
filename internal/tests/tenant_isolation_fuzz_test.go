package tests

import (
    "bytes"
    "context"
    "encoding/json"
    "math/rand"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "stellarbill-backend/internal/handlers"
    "stellarbill-backend/internal/reconciliation"
    "stellarbill-backend/internal/repository"
    "stellarbill-backend/internal/service"
    "github.com/gin-gonic/gin"
)

// deterministic seed used for reproducible fuzz runs
const fuzzSeed = 42

// fakeAdapter implements reconciliation.Adapter for tests.
type fakeAdapter struct{
    snaps []reconciliation.Snapshot
}

func (f *fakeAdapter) FetchSnapshots(ctx context.Context) ([]reconciliation.Snapshot, error) {
    out := make([]reconciliation.Snapshot, len(f.snaps))
    copy(out, f.snaps)
    return out, nil
}

func TestTenantIsolationFuzz(t *testing.T) {
    rand := rand.New(rand.NewSource(fuzzSeed))

    // tenants with overlapping prefix
    tenantA := "tenant-abc-001"
    tenantB := "tenant-abc-002"

    // subscriptions
    subA := &repository.SubscriptionRow{ID: "sub-collide-01", TenantID: tenantA, CustomerID: "cust-A", PlanID: "plan-1", Amount: "1000", Currency: "USD", Status: "active"}
    subB := &repository.SubscriptionRow{ID: "sub-collide-02", TenantID: tenantB, CustomerID: "cust-B", PlanID: "plan-1", Amount: "2000", Currency: "USD", Status: "active"}

    plan := &repository.PlanRow{ID: "plan-1", Name: "basic", Amount: "1000", Currency: "USD", Interval: "month"}

    // statements
    stmtA := &repository.StatementRow{ID: "stmt-A-1", SubscriptionID: subA.ID, CustomerID: subA.CustomerID, PeriodStart: time.Now().Add(-48*time.Hour).Format(time.RFC3339), PeriodEnd: time.Now().Add(-24*time.Hour).Format(time.RFC3339), IssuedAt: time.Now().Add(-24*time.Hour).Format(time.RFC3339), TotalAmount: "1000", Currency: "USD", Kind: "invoice", Status: "paid"}
    stmtB := &repository.StatementRow{ID: "stmt-B-1", SubscriptionID: subB.ID, CustomerID: subB.CustomerID, PeriodStart: time.Now().Add(-48*time.Hour).Format(time.RFC3339), PeriodEnd: time.Now().Add(-24*time.Hour).Format(time.RFC3339), IssuedAt: time.Now().Add(-24*time.Hour).Format(time.RFC3339), TotalAmount: "2000", Currency: "USD", Kind: "invoice", Status: "open"}

    // repos and services
    subRepo := repository.NewMockSubscriptionRepo(subA, subB)
    planRepo := repository.NewMockPlanRepo(plan)
    stmtRepo := repository.NewMockStatementRepo(stmtA, stmtB)

    subSvc := service.NewSubscriptionService(subRepo, planRepo)
    stmtSvc := service.NewStatementService(subRepo, stmtRepo)

    // reconciliation test store and adapter
    memStore := reconciliation.NewMemoryStore()
    snaps := []reconciliation.Snapshot{
        {SubscriptionID: subA.ID, TenantID: tenantA, Status: "active", Amount: 1000, Currency: "USD", Interval: "month", Balances: map[string]int64{"outstanding":0}, ExportedAt: time.Now()},
        {SubscriptionID: subB.ID, TenantID: tenantB, Status: "active", Amount: 2000, Currency: "USD", Interval: "month", Balances: map[string]int64{"outstanding":0}, ExportedAt: time.Now()},
    }
    adapter := &fakeAdapter{snaps: snaps}

    // seed some reports into memory store for list handler
    reconcilerSvc := reconciliation.NewService(adapter, memStore)
    backendSubs := []reconciliation.BackendSubscription{
        {SubscriptionID: subA.ID, TenantID: tenantA, Status: "active", Amount: 1000, Currency: "USD", Interval: "month", Balances: map[string]int64{"outstanding":0}, UpdatedAt: time.Now()},
        {SubscriptionID: subB.ID, TenantID: tenantB, Status: "active", Amount: 2000, Currency: "USD", Interval: "month", Balances: map[string]int64{"outstanding":0}, UpdatedAt: time.Now()},
    }
    if _, err := reconcilerSvc.Reconcile(context.Background(), backendSubs); err != nil {
        t.Fatalf("failed to reconcile initial reports: %v", err)
    }

    // HTTP handlers
    reconcileHandler := handlers.NewReconcileHandler(adapter, memStore)

    // random probe loop
    iterations := 250
    for i := 0; i < iterations; i++ {
        choice := rand.Intn(6)
        switch choice {
        case 0:
            // Subscription GetDetail: probe with mismatched tenant context
            target := subA
            if rand.Intn(2) == 0 { target = subB }
            tenantCtx := tenantA
            if rand.Intn(2) == 0 { tenantCtx = tenantB }
            callerID := target.CustomerID

            detail, _, err := subSvc.GetDetail(context.Background(), tenantCtx, callerID, target.ID)
            if err == nil && detail != nil {
                row, _ := subRepo.FindByID(context.Background(), detail.ID)
                if row.TenantID != tenantCtx {
                    t.Fatalf("subscription leak: tenantCtx=%s got detail for tenant=%s", tenantCtx, row.TenantID)
                }
            }

        case 1:
            // Statement GetDetail
            target := stmtA
            if rand.Intn(2) == 0 { target = stmtB }
            roles := []string{}
            r := rand.Intn(3)
            var caller string
            if r == 0 {
                roles = []string{"admin"}
                caller = "admin-1"
            } else if r == 1 {
                roles = []string{"merchant"}
                caller = tenantA
                if rand.Intn(2) == 0 { caller = tenantB }
            } else {
                caller = target.CustomerID
            }

            detail, _, err := stmtSvc.GetDetail(context.Background(), caller, roles, target.ID)
            if err == nil && detail != nil {
                subRow, _ := subRepo.FindByID(context.Background(), detail.SubscriptionID)
                isAdmin := false
                for _, rr := range roles { if rr=="admin" { isAdmin = true } }
                if !isAdmin {
                    if contains(roles, "merchant") {
                        if subRow.TenantID != caller {
                            t.Fatalf("merchant leak: caller tenant=%s saw statement for tenant=%s", caller, subRow.TenantID)
                        }
                    } else {
                        if caller != detail.Customer {
                            t.Fatalf("subscriber leak: caller=%s saw statement for customer=%s", caller, detail.Customer)
                        }
                    }
                }
            }

        case 2:
            // ListByCustomer
            cust := stmtA.CustomerID
            if rand.Intn(2)==0 { cust = stmtB.CustomerID }
            roles := []string{}
            caller := cust
            if rand.Intn(2)==0 {
                roles = []string{"merchant"}
                caller = tenantA
                if rand.Intn(2)==0 { caller = tenantB }
            }
            list, _, _, err := stmtSvc.ListByCustomer(context.Background(), caller, roles, cust, repository.StatementQuery{})
            if err == nil && list != nil {
                for _, s := range list.Statements {
                    if s.Customer != cust {
                        t.Fatalf("ListByCustomer leak: asked for customer=%s got customer=%s", cust, s.Customer)
                    }
                    if contains(roles, "merchant") {
                        subRow, _ := subRepo.FindByID(context.Background(), s.SubscriptionID)
                        if subRow.TenantID != caller {
                            t.Fatalf("ListByCustomer merchant leak: merchant=%s saw tenant=%s", caller, subRow.TenantID)
                        }
                    }
                }
            }

        case 3:
            // Reconcile handler: non-admin should be forbidden if backend contains other-tenant entries
            backend := []reconciliation.BackendSubscription{{SubscriptionID: subB.ID, TenantID: tenantB, Status: "active", Amount:2000, Currency: "USD", Interval: "month", Balances: map[string]int64{"a":0}, UpdatedAt: time.Now()}}
            bts, _ := json.Marshal(backend)
            req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(bts))
            w := httptest.NewRecorder()
            gin.SetMode(gin.TestMode)
            c, _ := gin.CreateTestContext(w)
            c.Request = req
            c.Set("tenantID", tenantA)
            c.Set("role", "merchant")
            reconcileHandler(c)
            if w.Code == http.StatusOK {
                var out map[string]interface{}
                _ = json.Unmarshal(w.Body.Bytes(), &out)
                if reports, ok := out["reports"].([]interface{}); ok {
                    for _, r := range reports {
                        m := r.(map[string]interface{})
                        if tid, ok := m["tenant_id"].(string); ok && tid == tenantB {
                            t.Fatalf("reconcile handler leak: merchant tenant %s saw report for tenant %s", tenantA, tenantB)
                        }
                    }
                }
            }

        case 4:
            // ListReportsHandler: validate store.ListReportsByTenant
            reports, _ := memStore.ListReportsByTenant(tenantA)
            for _, r := range reports {
                if r.TenantID != tenantA {
                    t.Fatalf("ListReportsByTenant leak: tenantA saw tenant %s", r.TenantID)
                }
            }

        default:
            // noop
        }
    }
}

func contains(arr []string, want string) bool {
    for _, s := range arr {
        if s == want { return true }
    }
    return false
}
