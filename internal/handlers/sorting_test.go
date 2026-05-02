package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestListPlans_StableSortByID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}

	// Return plans in reverse order; expect them sorted ascending by ID.
	mockSvc.On("ListPlans", mock.Anything).Return([]Plan{
		{ID: "plan_z", Name: "Z"},
		{ID: "plan_a", Name: "A"},
		{ID: "plan_m", Name: "M"},
	}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/plans", nil)

	h.ListPlans(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string][]Plan
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	plans := resp["plans"]
	assert.Equal(t, "plan_a", plans[0].ID)
	assert.Equal(t, "plan_m", plans[1].ID)
	assert.Equal(t, "plan_z", plans[2].ID)
}

func TestListSubscriptions_StableSortByID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockSvc := new(MockSubscriptionService)
	h := &Handler{Subscriptions: mockSvc}

	// Return subscriptions in reverse order; expect them sorted ascending by ID.
	mockSvc.On("ListSubscriptions", mock.Anything).Return([]Subscription{
		{ID: "sub_z", Status: "active"},
		{ID: "sub_a", Status: "active"},
		{ID: "sub_m", Status: "active"},
	}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)

	h.ListSubscriptions(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string][]Subscription
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	subs := resp["subscriptions"]
	assert.Equal(t, "sub_a", subs[0].ID)
	assert.Equal(t, "sub_m", subs[1].ID)
	assert.Equal(t, "sub_z", subs[2].ID)
}

func TestListPlans_NilResultReturnsEmptyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	h.ListPlans(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string][]Plan
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["plans"])
}

func TestListSubscriptions_NilResultReturnsEmptyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockSvc := new(MockSubscriptionService)
	h := &Handler{Subscriptions: mockSvc}
	mockSvc.On("ListSubscriptions", mock.Anything).Return(nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	h.ListSubscriptions(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string][]Subscription
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["subscriptions"])
}
