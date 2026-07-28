package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newPlans(n int) []Plan {
	plans := make([]Plan, 0, n)
	names := []string{"a", "b", "c", "d", "e", "f"}
	for i := 0; i < n; i++ {
		plans = append(plans, Plan{ID: "plan_" + names[i], Name: names[i], Amount: "10.00", Currency: "USD", Interval: "month"})
	}
	return plans
}

func TestListPlans_LinkHeader_FirstPage_OmitsPrevIncludesNext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return(newPlans(6), nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/plans?limit=2", nil)

	h.ListPlans(c)

	link := w.Header().Get("Link")
	assert.NotEmpty(t, link)
	assert.Contains(t, link, `rel="first"`)
	assert.Contains(t, link, `rel="next"`)
	assert.NotContains(t, link, `rel="prev"`)
}

func TestListPlans_LinkHeader_LastPage_OmitsNextIncludesPrev(t *testing.T) {
	gin.SetMode(gin.TestMode)

	plans := newPlans(3)
	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return(plans, nil)

	// First, fetch page 1 to obtain a real cursor into page 2 (the last page
	// given limit=2 and 3 items).
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/plans?limit=2", nil)
	h.ListPlans(c1)

	cursor := extractNextCursorFromLink(t, w1.Header().Get("Link"))

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/plans?limit=2&cursor="+cursor, nil)
	h.ListPlans(c2)

	link := w2.Header().Get("Link")
	assert.NotEmpty(t, link)
	assert.Contains(t, link, `rel="first"`)
	assert.Contains(t, link, `rel="prev"`)
	assert.NotContains(t, link, `rel="next"`)
}

func TestListPlans_LinkHeader_EmptyResult_OnlyFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return([]Plan{}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/plans", nil)

	h.ListPlans(c)

	link := w.Header().Get("Link")
	assert.Contains(t, link, `rel="first"`)
	assert.NotContains(t, link, `rel="next"`)
	assert.NotContains(t, link, `rel="prev"`)
}

func TestListPlans_LinkHeader_PreservesQueryFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return(newPlans(4), nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/plans?limit=2&currency=usd", nil)

	h.ListPlans(c)

	link := w.Header().Get("Link")
	assert.Contains(t, link, "limit=2")
	assert.Contains(t, link, "currency=usd")
}

func TestListPlans_LinkHeader_AbsentWhenNoRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockSvc := new(MockPlanService)
	h := &Handler{Plans: mockSvc}
	mockSvc.On("ListPlans", mock.Anything).Return(newPlans(1), nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w) // c.Request is left nil, as some existing tests do

	assert.NotPanics(t, func() { h.ListPlans(c) })
	assert.Empty(t, w.Header().Get("Link"))
}

func TestListSubscriptions_LinkHeader_FirstPage_OmitsPrev(t *testing.T) {
	gin.SetMode(gin.TestMode)

	subs := []Subscription{
		{ID: "sub_1", Customer: "a"}, {ID: "sub_2", Customer: "b"}, {ID: "sub_3", Customer: "c"},
	}
	mockSvc := new(MockSubscriptionService)
	h := &Handler{Subscriptions: mockSvc}
	mockSvc.On("ListSubscriptions", mock.Anything).Return(subs, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/subscriptions?limit=2", nil)

	h.ListSubscriptions(c)

	link := w.Header().Get("Link")
	assert.Contains(t, link, `rel="first"`)
	assert.Contains(t, link, `rel="next"`)
	assert.NotContains(t, link, `rel="prev"`)
}

// extractNextCursorFromLink pulls the cursor query parameter out of the
// rel="next" link target, so a test can follow it to the next page.
func extractNextCursorFromLink(t *testing.T, link string) string {
	t.Helper()
	for _, part := range strings.Split(link, ", ") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end < 0 {
			t.Fatalf("malformed link part: %q", part)
		}
		target := part[start+1 : end]
		idx := strings.Index(target, "cursor=")
		if idx < 0 {
			t.Fatalf("no cursor param in next link: %q", target)
		}
		cursorAndRest := target[idx+len("cursor="):]
		if amp := strings.Index(cursorAndRest, "&"); amp >= 0 {
			return cursorAndRest[:amp]
		}
		return cursorAndRest
	}
	t.Fatalf("no rel=next link found in %q", link)
	return ""
}
