package service

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/repository"
)

// tenantFixture holds seeded data for two distinct tenants used in property-based tests.
type tenantFixture struct {
	tenantA string
	tenantB string
	subsA    []string
	subsB    []string
	stmtsA   []string
	stmtsB   []string
}

// seedMultiTenantFixture seeds two tenants with subscriptions and statements.
func seedMultiTenantFixture(t *testing.T, mockSQL *sqlmock.Sqlmock) *tenantFixture {
	t.Helper()
	f := &tenantFixture{
		tenantA: "tenant-alpha",
		tenantB: "tenant-beta",
	}

	// Seed Tenant A: 5 subscriptions and 10 statements
	for i := 0; i < 5; i++ {
		f.subsA = append(f.subsA, fmt.Sprintf("sub-a-%d", i))
	}
	for i := 0; i < 10; i++ {
		f.stmtsA = append(f.stmtsA, fmt.Sprintf("stmt-a-%d", i))
	}

	// Seed Tenant B: 5 subscriptions and 10 statements
	for i := 0; i < 5; i++ {
		f.subsB = append(f.subsB, fmt.Sprintf("sub-b-%d", i))
	}
	for i := 0; i < 10; i++ {
		f.stmtsB = append(f.stmtsB, fmt.Sprintf("stmt-b-%d", i))
	}

	return f
}

// TestTenantIsolation_PropertyRead exercises tenant scoping across the service layer.
// It ensures that a caller in tenant A can never receive data created by tenant B
// when calling service methods that handle plans, subscriptions, and statements.
func TestTenantIsolation_PropertyRead(t *testing.T) {
	// Fixed seed for deterministic failure reporting
	rand.Seed(42)

	// Initialize service dependencies with mock repos
	subRepo := repository.NewMockSubscriptionRepo()
	subSvc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	stmtRepo := repository.NewMockStatementRepo()
	stmtSvc := service.NewStatementService(stmtRepo, repository.NewMockSubscriptionRepo())

	// Initialize fixture with seeded data
	f := &tenantFixture{
		tenantA: "tenant-alpha",
		tenantB: "tenant-beta",
		subsA:    []string{"sub-a-1", "sub-a-2"},
		subsB:    []string{"sub-b-1", "sub-b-2"},
		stmtsA:   []string{"stmt-a-1", "stmt-a-2"},
		stmtsB:   []string{"stmt-b-1", "stmt-b-2"},
	}

	// Seed subscriptions into mock repo
	for _, id := range f.subsA {
		subRepo.records[id] = &repository.SubscriptionRow{ID: id, TenantID: f.tenantA, Status: "active"}
	}
	for _, id := range f.subsB {
		subRepo.records[id] = &repository.SubscriptionRow{ID: id, TenantID: f.tenantB, Status: "active"}
	}

	// Seed statements into mock repo
	for _, id := range f.stmtsA {
		stmtRepo.records[id] = &repository.StatementRow{ID: id, TenantID: f.tenantA}
	}
	for _, id := range f.stmtsB {
		stmtRepo.records[id] = &repository.StatementRow{ID: id, TenantID: f.tenantB}
	}

	t.Run("SubscriptionGetDetail_CrossTenantIsolation", func(t *testing.T) {
		for i := 0; i < 200; i++ {
			// Caller from Tenant A tries to access Tenant B's subscription
			targetSub := f.subsB[rand.Intn(len(f.subsB))]
			
			// service.SubscriptionService.GetDetail takes (ctx, tenantID, callerID, subID)
			detail, _, err := subSvc.GetDetail(context.Background(), f.tenantA, "caller-a", targetSub)
			
			// Core property check: must reject cross-tenant access
			if err == nil {
				t.Errorf("Iteration %d: should have rejected access to %s from tenant %s", i, targetSub, f.tenantA)
			}
			if detail != nil {
				t.Errorf("Iteration %d: returned detail for %s from tenant %s", i, targetSub, f.tenantA)
			}
		}
	})

	t.Run("StatementList_CrossTenantIsolation", func(t *testing.T) {
		stmtRepo := repository.NewMockStatementRepo()
		stmtSvc := service.NewStatementService(stmtRepo, repository.NewMockSubscriptionRepo())

		// Seed statements for A and B into the mock repo
		for _, id := range f.stmtsA {
			stmtRepo.records[id] = &repository.StatementRow{ID: id, TenantID: f.tenantA}
		}
		for _, id := range f.stmtsB {
			stmtRepo.records[id] = &repository.StatementRow{ID: id, TenantID: f.tenantB}
		}

		for i := 0; i < 200; i++ {
			// Request statements for a customer in Tenant B, using context from Tenant A
			q := repository.StatementQuery{Limit: 10}
			detail, count, _, err := stmtSvc.ListByCustomer(context.Background(), "caller-a", []string{"customer"}, "cust-b", q)
			
			// Property check: if no error, results must NOT contain Tenant B's statements
			if err == nil && count > 0 {
				for _, s := range detail.Statements {
					if s.TenantID == f.tenantB {
						t.Errorf("Iteration %d: Leaked statement %s from Tenant B to Tenant A", i, s.ID)
					}
				}
			}
		}
	})
}