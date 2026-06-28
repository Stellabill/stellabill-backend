package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/testutil/golden"
)

func TestListStatements_Golden(t *testing.T) {
	tests := []struct {
		name           string
		mockSvc        *mockStatementsTestService
		query          string
		goldenFilename string
	}{
		{
			name: "Standard List",
			mockSvc: &mockStatementsTestService{
				listDetail: &service.ListStatementsDetail{
					Statements: []*service.StatementDetail{
						{ID: "stmt-1", Kind: "invoice", Status: "paid", TotalAmount: "1000", Currency: "USD"},
						{ID: "stmt-2", Kind: "receipt", Status: "pending", TotalAmount: "2000", Currency: "USD"},
					},
				},
				count: 2,
			},
			query:          "",
			goldenFilename: "testdata/list_statements_standard.golden",
		},
		{
			name: "Empty Result",
			mockSvc: &mockStatementsTestService{
				listDetail: &service.ListStatementsDetail{
					Statements: []*service.StatementDetail{},
				},
				count: 0,
			},
			query:          "",
			goldenFilename: "testdata/list_statements_empty.golden",
		},
		{
			name: "Pagination Cursor",
			mockSvc: &mockStatementsTestService{
				listDetail: &service.ListStatementsDetail{
					Statements: []*service.StatementDetail{
						{ID: "stmt-3", Kind: "invoice", Status: "paid", TotalAmount: "3000", Currency: "USD"},
						{ID: "stmt-4", Kind: "receipt", Status: "pending", TotalAmount: "4000", Currency: "USD"},
					},
				},
				count: 10,
			},
			query:          "?page=2&page_size=2",
			goldenFilename: "testdata/list_statements_paginated.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := stmtRouter(tt.mockSvc, true)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/api/statements"+tt.query, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			golden.AssertJSON(t, w.Body.Bytes(), tt.goldenFilename)
		})
	}
}
