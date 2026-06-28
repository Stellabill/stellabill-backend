package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"stellarbill-backend/internal/testutil/golden"
)

func TestListPlans_Golden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		mockPlans      []Plan
		query          string
		goldenFilename string
	}{
		{
			name: "Standard List",
			mockPlans: []Plan{
				{ID: "plan_1", Name: "Basic", Amount: "10.00", Currency: "USD", Interval: "month"},
				{ID: "plan_2", Name: "Pro", Amount: "29.99", Currency: "USD", Interval: "month"},
			},
			query:          "",
			goldenFilename: "testdata/list_plans_standard.golden",
		},
		{
			name:           "Empty Result",
			mockPlans:      []Plan{},
			query:          "",
			goldenFilename: "testdata/list_plans_empty.golden",
		},
		{
			name: "Pagination Cursor",
			mockPlans: []Plan{
				{ID: "plan_1", Name: "Basic", Amount: "10.00", Currency: "USD", Interval: "month"},
				{ID: "plan_2", Name: "Pro", Amount: "29.99", Currency: "USD", Interval: "month"},
				{ID: "plan_3", Name: "Enterprise", Amount: "99.99", Currency: "USD", Interval: "month"},
			},
			query:          "?limit=2",
			goldenFilename: "testdata/list_plans_paginated.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := new(MockPlanService)
			h := &Handler{Plans: mockSvc}

			mockSvc.On("ListPlans", mock.Anything).Return(tt.mockPlans, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest("GET", "/plans"+tt.query, nil)
			c.Request = req

			h.ListPlans(c)

			assert.Equal(t, http.StatusOK, w.Code)
			golden.AssertJSON(t, w.Body.Bytes(), tt.goldenFilename)
		})
	}
}
