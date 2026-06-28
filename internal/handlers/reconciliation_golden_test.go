package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/testutil/golden"
)

type mockReportStore struct {
	reports []reconciliation.Report
}

func (m *mockReportStore) SaveReports(reports []reconciliation.Report) error {
	return nil
}

func (m *mockReportStore) ListReports() ([]reconciliation.Report, error) {
	return m.reports, nil
}

func (m *mockReportStore) ListReportsByTenant(tenantID string) ([]reconciliation.Report, error) {
	var filtered []reconciliation.Report
	for _, r := range m.reports {
		if r.TenantID == tenantID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func TestListReports_Golden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		url     string
		reports []reconciliation.Report
		golden  string
	}{
		{
			name:   "Standard List",
			url:    "/reports",
			golden: "testdata/list_reports_standard.golden",
			reports: []reconciliation.Report{
				{
					SubscriptionID: "sub_1",
					TenantID:       "tenant_1",
					Matched:        true,
					Mismatches:     nil,
					Backend: reconciliation.BackendSubscription{
						SubscriptionID: "sub_1",
						Status:         "active",
						Amount:         1000,
						Currency:       "USD",
						Interval:       "month",
						UpdatedAt:      time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					},
					Contract: reconciliation.Snapshot{
						SubscriptionID: "sub_1",
						Status:         "active",
						Amount:         1000,
						Currency:       "USD",
						Interval:       "month",
						ExportedAt:     time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					},
				},
				{
					SubscriptionID: "sub_2",
					TenantID:       "tenant_1",
					Matched:        false,
					Mismatches: []reconciliation.FieldMismatch{
						{Field: "status", BackendValue: "active", ContractValue: "canceled"},
					},
					Backend: reconciliation.BackendSubscription{
						SubscriptionID: "sub_2",
						Status:         "active",
						Amount:         500,
						Currency:       "EUR",
						Interval:       "month",
						UpdatedAt:      time.Date(2023, 1, 2, 12, 0, 0, 0, time.UTC),
					},
					Contract: reconciliation.Snapshot{
						SubscriptionID: "sub_2",
						Status:         "canceled",
						Amount:         500,
						Currency:       "EUR",
						Interval:       "month",
						ExportedAt:     time.Date(2023, 1, 2, 12, 0, 0, 0, time.UTC),
					},
				},
			},
		},
		{
			name:    "Empty Result",
			url:     "/reports",
			golden:  "testdata/list_reports_empty.golden",
			reports: []reconciliation.Report{},
		},
		{
			name:   "Pagination Cursor",
			url:    "/reports?limit=1",
			golden: "testdata/list_reports_paginated.golden",
			reports: []reconciliation.Report{
				{
					SubscriptionID: "sub_3",
					TenantID:       "tenant_1",
					Matched:        true,
					Backend: reconciliation.BackendSubscription{
						SubscriptionID: "sub_3",
						Status:         "active",
						UpdatedAt:      time.Date(2023, 1, 3, 12, 0, 0, 0, time.UTC),
					},
					Contract: reconciliation.Snapshot{
						SubscriptionID: "sub_3",
						Status:         "active",
						ExportedAt:     time.Date(2023, 1, 3, 12, 0, 0, 0, time.UTC),
					},
				},
				{
					SubscriptionID: "sub_4",
					TenantID:       "tenant_1",
					Matched:        true,
					Backend: reconciliation.BackendSubscription{
						SubscriptionID: "sub_4",
						Status:         "canceled",
						UpdatedAt:      time.Date(2023, 1, 4, 12, 0, 0, 0, time.UTC),
					},
					Contract: reconciliation.Snapshot{
						SubscriptionID: "sub_4",
						Status:         "canceled",
						ExportedAt:     time.Date(2023, 1, 4, 12, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockReportStore{reports: tt.reports}
			handler := NewListReportsHandler(store)

			router := gin.New()
			router.GET("/reports", func(c *gin.Context) {
				c.Set("callerID", "test-admin")
				c.Set("tenantID", "tenant_1")
				c.Set(auth.RolesContextKey, []auth.Role{auth.RoleAdmin})
				handler(c)
			})

			req, _ := http.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			
			golden.AssertJSON(t, w.Body.Bytes(), tt.golden)
		})
	}
}
