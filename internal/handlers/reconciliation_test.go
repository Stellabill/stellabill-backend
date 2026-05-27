package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"
)

func mockExtractRoleMiddleware(role, tenantID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// auth.ExtractRole uses X-Role header
		if role != "" {
			c.Request.Header.Set("X-Role", role)
		}
		c.Set("tenantID", tenantID)
		// Set callerID for thoroughness
		c.Set("callerID", "test-user")
		c.Next()
	}
}

func TestReconcileHandler_TenantScopingAndRBAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()

	tests := []struct {
		name              string
		role              string
		contextTenant     string
		backendSubs       []reconciliation.BackendSubscription
		snapshots         []reconciliation.Snapshot
		expectedCode      int
		expectedReports   int
		expectedTenantIDs []string // check stored reports
	}{
		{
			name:          "Admin can view multiple tenants and explicitly set tenant",
			role:          string(auth.RoleAdmin),
			contextTenant: "admin-tenant",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", UpdatedAt: now},
				{SubscriptionID: "sub-2", TenantID: "tenant-b", Status: "active", UpdatedAt: now},
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-2", TenantID: "tenant-b", Status: "active", ExportedAt: now},
			},
			expectedCode:      http.StatusOK,
			expectedReports:   2,
			expectedTenantIDs: []string{"tenant-a", "tenant-b"},
		},
		{
			name:          "Merchant can reconcile own tenant",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "merchant-a", Status: "active", UpdatedAt: now},
				{SubscriptionID: "sub-2", TenantID: "", Status: "active", UpdatedAt: now}, // gets stamped
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "merchant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-2", TenantID: "merchant-a", Status: "active", ExportedAt: now},
				{SubscriptionID: "sub-other", TenantID: "merchant-b", Status: "active", ExportedAt: now}, // should be filtered out
			},
			expectedCode:      http.StatusOK,
			expectedReports:   2,
			expectedTenantIDs: []string{"merchant-a", "merchant-a"},
		},
		{
			name:          "Merchant cannot reconcile cross-tenant",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "merchant-b", Status: "active", UpdatedAt: now},
			},
			snapshots:       []reconciliation.Snapshot{},
			expectedCode:    http.StatusForbidden,
			expectedReports: 0,
		},
		{
			name:          "Merchant sees only own snapshots",
			role:          string(auth.RoleMerchant),
			contextTenant: "merchant-a",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-b", TenantID: "", Status: "active", UpdatedAt: now}, // stamped as merchant-a
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-b", TenantID: "merchant-b", Status: "active", ExportedAt: now}, // filtered out
			},
			expectedCode:    http.StatusOK,
			expectedReports: 1, // Will create 1 mismatch report due to missing snapshot
			expectedTenantIDs: []string{"merchant-a"},
		},
		{
			name:          "Missing manage:reconciliation permission -> 403",
			role:          string(auth.RoleUser), // RoleUser does not have PermManageReconciliation
			contextTenant: "tenant-user",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-user", Status: "active", UpdatedAt: now},
			},
			snapshots:       []reconciliation.Snapshot{},
			expectedCode:    http.StatusForbidden,
			expectedReports: 0,
		},
		{
			name:          "Missing TenantID defaults safely",
			role:          string(auth.RoleAdmin),
			contextTenant: "",
			backendSubs: []reconciliation.BackendSubscription{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", UpdatedAt: now},
			},
			snapshots: []reconciliation.Snapshot{
				{SubscriptionID: "sub-1", TenantID: "tenant-a", Status: "active", ExportedAt: now},
			},
			expectedCode:      http.StatusOK,
			expectedReports:   1,
			expectedTenantIDs: []string{"tenant-a"},
		},
		{
			name:          "Malformed JSON Request Body -> 400",
			role:          string(auth.RoleAdmin),
			contextTenant: "admin",
			backendSubs:   nil, // we will send malformed text
			snapshots:     []reconciliation.Snapshot{},
			expectedCode:  http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := reconciliation.NewMemoryAdapter(tc.snapshots...)
			store := reconciliation.NewMemoryStore()

			r := gin.New()
			r.Use(mockExtractRoleMiddleware(tc.role, tc.contextTenant))
			r.POST("/reconcile", auth.RequirePermission(auth.PermManageReconciliation), NewReconcileHandler(adapter, store))

			var payload []byte
			if tc.name == "Malformed JSON Request Body -> 400" {
				payload = []byte("{bad-json")
			} else {
				payload, _ = json.Marshal(tc.backendSubs)
			}

			req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Fatalf("expected code %d, got %d. Body: %s", tc.expectedCode, w.Code, w.Body.String())
			}

			if w.Code == http.StatusOK {
				var resp struct {
					Reports []reconciliation.Report `json:"reports"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				if len(resp.Reports) != tc.expectedReports {
					t.Fatalf("expected %d reports, got %d", tc.expectedReports, len(resp.Reports))
				}

				saved, err := store.ListReports()
				if err != nil {
					t.Fatalf("store.ListReports error: %v", err)
				}
				if len(saved) != tc.expectedReports {
					t.Fatalf("expected %d saved reports, got %d", tc.expectedReports, len(saved))
				}

				for i, expectedTenant := range tc.expectedTenantIDs {
					if saved[i].TenantID != expectedTenant {
						t.Errorf("expected tenant %s for report %d, got %s", expectedTenant, i, saved[i].TenantID)
					}
					if resp.Reports[i].TenantID != expectedTenant {
						t.Errorf("expected tenant %s in response report %d, got %s", expectedTenant, i, resp.Reports[i].TenantID)
					}
				}
			}
		})
	}
}
