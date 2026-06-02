package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// setupSpanRecorder installs an in-memory span recorder as the global tracer provider.
func setupSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	return sr
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}

// TestGetSubscriptionSpans verifies handler→service span propagation end-to-end.
func TestGetSubscriptionSpans(t *testing.T) {
	sr := setupSpanRecorder(t)

	subRow := &repository.SubscriptionRow{
		ID:         "sub-1",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "caller-1",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "monthly",
	}
	planRow := &repository.PlanRow{
		ID:       "plan-1",
		Name:     "Basic",
		Amount:   "1000",
		Currency: "USD",
		Interval: "monthly",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(subRow),
		repository.NewMockPlanRepo(planRow),
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/subscriptions/:id", func(c *gin.Context) {
		c.Set("callerID", "caller-1")
		c.Set("tenantID", "tenant-1")
	}, NewGetSubscriptionHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/subscriptions/sub-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	spans := sr.Ended()
	names := spanNames(spans)

	assert.Contains(t, names, "handler.GetSubscription", "handler span must be recorded")
	assert.Contains(t, names, "SubscriptionService.GetDetail", "service span must be recorded")

	// All spans must share the same trace ID (end-to-end propagation).
	require.GreaterOrEqual(t, len(spans), 2)
	traceID := spans[0].SpanContext().TraceID()
	for _, s := range spans[1:] {
		assert.Equal(t, traceID, s.SpanContext().TraceID(),
			"span %q must share trace ID with root span", s.Name())
	}
}

// TestChangeSubscriptionStatusSpans verifies handler→service span propagation
// for the status-change path.
func TestChangeSubscriptionStatusSpans(t *testing.T) {
	sr := setupSpanRecorder(t)

	subRow := &repository.SubscriptionRow{
		ID:         "sub-2",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "caller-1",
		Status:     "active",
		Amount:     "500",
		Currency:   "USD",
		Interval:   "monthly",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(subRow),
		repository.NewMockPlanRepo(),
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PATCH("/subscriptions/:id/status", func(c *gin.Context) {
		c.Set("tenantID", "tenant-1")
		c.Set("callerID", "caller-1")
	}, NewChangeSubscriptionStatusHandler(svc))

	body, _ := json.Marshal(map[string]string{"status": "cancelled"})
	req := httptest.NewRequest(http.MethodPatch, "/subscriptions/sub-2/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := sr.Ended()
	names := spanNames(spans)

	assert.Contains(t, names, "handler.ChangeSubscriptionStatus", "handler span must be recorded")
	assert.Contains(t, names, "SubscriptionService.ChangeStatus", "service span must be recorded")

	require.GreaterOrEqual(t, len(spans), 2)
	traceID := spans[0].SpanContext().TraceID()
	for _, s := range spans[1:] {
		assert.Equal(t, traceID, s.SpanContext().TraceID(),
			"span %q must share trace ID", s.Name())
	}
}

// TestListPlansHandlerSpan verifies that Handler.ListPlans records a span.
func TestListPlansHandlerSpan(t *testing.T) {
	sr := setupSpanRecorder(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockPlans := new(MockPlanService)
	mockPlans.On("ListPlans", mock.Anything).Return([]Plan{}, nil)

	h := &Handler{Plans: mockPlans}
	r.GET("/plans", h.ListPlans)

	req := httptest.NewRequest(http.MethodGet, "/plans", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, spanNames(sr.Ended()), "handler.ListPlans")
}

// TestListSubscriptionsHandlerSpan verifies that Handler.ListSubscriptions records a span.
func TestListSubscriptionsHandlerSpan(t *testing.T) {
	sr := setupSpanRecorder(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockSubs := new(MockSubscriptionService)
	mockSubs.On("ListSubscriptions", mock.Anything).Return([]Subscription{}, nil)

	h := &Handler{Subscriptions: mockSubs}
	r.GET("/subscriptions", h.ListSubscriptions)

	req := httptest.NewRequest(http.MethodGet, "/subscriptions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, spanNames(sr.Ended()), "handler.ListSubscriptions")
}
