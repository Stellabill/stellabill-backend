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

func TestListSubscriptions_Golden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		mockSubs       []Subscription
		query          string
		goldenFilename string
	}{
		{
			name: "Standard List",
			mockSubs: []Subscription{
				{ID: "sub_1", PlanID: "plan_1", Customer: "Alice", Status: "active"},
				{ID: "sub_2", PlanID: "plan_2", Customer: "Bob", Status: "canceled"},
			},
			query:          "",
			goldenFilename: "testdata/list_subscriptions_standard.golden",
		},
		{
			name:           "Empty Result",
			mockSubs:       []Subscription{},
			query:          "",
			goldenFilename: "testdata/list_subscriptions_empty.golden",
		},
		{
			name: "Pagination Cursor",
			mockSubs: []Subscription{
				{ID: "sub_1", PlanID: "plan_1", Customer: "Alice", Status: "active"},
				{ID: "sub_2", PlanID: "plan_2", Customer: "Bob", Status: "canceled"},
				{ID: "sub_3", PlanID: "plan_3", Customer: "Charlie", Status: "past_due"},
			},
			query:          "?limit=2",
			goldenFilename: "testdata/list_subscriptions_paginated.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := new(MockSubscriptionService)
			h := &Handler{Subscriptions: mockSvc}

			mockSvc.On("ListSubscriptions", mock.Anything).Return(tt.mockSubs, nil)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest("GET", "/subscriptions"+tt.query, nil)
			c.Request = req

			h.ListSubscriptions(c)

			assert.Equal(t, http.StatusOK, w.Code)
			golden.AssertJSON(t, w.Body.Bytes(), tt.goldenFilename)
		})
	}
}
