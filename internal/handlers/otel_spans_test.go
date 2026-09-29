package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/tracing"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

func findStubByName(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

func getAttributeValue(attrs []attribute.KeyValue, key string) string {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

// TestGetSubscriptionSpans verifies handler→service→repo span propagation end-to-end using InMemoryExporter.
func TestGetSubscriptionSpans(t *testing.T) {
	exporter, shutdown := tracing.SetupTestTracerProvider()
	defer shutdown()

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
	r.GET("/api/v1/subscriptions/:id", func(c *gin.Context) {
		c.Set("callerID", "caller-1")
		c.Set("tenantID", "tenant-1")
	}, NewGetSubscriptionHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	spans := exporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 3)

	handlerStub, found := findStubByName(spans, "handler.GetSubscription")
	require.True(t, found, "handler span must be recorded")

	serviceStub, found := findStubByName(spans, "SubscriptionService.GetDetail")
	require.True(t, found, "service span must be recorded")

	subRepoStub, found := findStubByName(spans, "SubscriptionRepo.FindByID")
	require.True(t, found, "subscription repo span must be recorded")

	planRepoStub, found := findStubByName(spans, "PlanRepo.FindByID")
	require.True(t, found, "plan repo span must be recorded")

	// Single TraceID shared across handler, service, and repo spans
	traceID := handlerStub.SpanContext.TraceID()
	require.True(t, traceID.IsValid(), "trace ID must be valid")
	assert.Equal(t, traceID, serviceStub.SpanContext.TraceID())
	assert.Equal(t, traceID, subRepoStub.SpanContext.TraceID())
	assert.Equal(t, traceID, planRepoStub.SpanContext.TraceID())

	// Parent-child hierarchy: Handler -> Service -> Repositories
	assert.Equal(t, handlerStub.SpanContext.SpanID(), serviceStub.Parent.SpanID(), "service span parent must be handler span")
	assert.Equal(t, serviceStub.SpanContext.SpanID(), subRepoStub.Parent.SpanID(), "sub repo span parent must be service span")
	assert.Equal(t, serviceStub.SpanContext.SpanID(), planRepoStub.Parent.SpanID(), "plan repo span parent must be service span")

	// Key span attributes
	assert.Equal(t, "sub-1", getAttributeValue(serviceStub.Attributes, "subscription.id"))
	assert.Equal(t, "tenant-1", getAttributeValue(serviceStub.Attributes, "tenant.id"))
	assert.Equal(t, "caller-1", getAttributeValue(serviceStub.Attributes, "caller.id"))

	assert.Equal(t, "sub-1", getAttributeValue(subRepoStub.Attributes, "subscription.id"))
	assert.Equal(t, "tenant-1", getAttributeValue(subRepoStub.Attributes, "tenant.id"))

	assert.Equal(t, "plan-1", getAttributeValue(planRepoStub.Attributes, "plan.id"))
}

// TestGetSubscriptionSpans_SamplingOff verifies that when sampling is disabled, no spans are exported.
func TestGetSubscriptionSpans_SamplingOff(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	subRow := &repository.SubscriptionRow{
		ID:         "sub-sampling-off",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "caller-1",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "monthly",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(subRow),
		repository.NewMockPlanRepo(&repository.PlanRow{ID: "plan-1"}),
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/subscriptions/:id", func(c *gin.Context) {
		c.Set("callerID", "caller-1")
		c.Set("tenantID", "tenant-1")
	}, NewGetSubscriptionHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub-sampling-off", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, exporter.GetSpans(), "no spans should be recorded when sampling is disabled")
}

// TestGetSubscriptionSpans_ErrorStatus verifies error span status marking on failure.
func TestGetSubscriptionSpans_ErrorStatus(t *testing.T) {
	exporter, shutdown := tracing.SetupTestTracerProvider()
	defer shutdown()

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(),
		repository.NewMockPlanRepo(),
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/subscriptions/:id", func(c *gin.Context) {
		c.Set("callerID", "caller-1")
		c.Set("tenantID", "tenant-1")
	}, NewGetSubscriptionHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/non-existent-sub", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans)

	serviceStub, found := findStubByName(spans, "SubscriptionService.GetDetail")
	require.True(t, found)
	assert.Equal(t, codes.Error, serviceStub.Status.Code, "error status must be set on service span for missing subscription")

	handlerStub, found := findStubByName(spans, "handler.GetSubscription")
	require.True(t, found)
	assert.Equal(t, codes.Error, handlerStub.Status.Code, "error status must be set on handler span for 404 response")
}

// TestGetSubscriptionSpans_MissingParentContextFallback verifies root span creation when traceparent header is absent.
func TestGetSubscriptionSpans_MissingParentContextFallback(t *testing.T) {
	exporter, shutdown := tracing.SetupTestTracerProvider()
	defer shutdown()

	subRow := &repository.SubscriptionRow{
		ID:         "sub-fallback",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "caller-1",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "monthly",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(subRow),
		repository.NewMockPlanRepo(&repository.PlanRow{ID: "plan-1"}),
	)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/subscriptions/:id", func(c *gin.Context) {
		c.Set("callerID", "caller-1")
		c.Set("tenantID", "tenant-1")
	}, NewGetSubscriptionHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub-fallback", nil)
	// Deliberately no traceparent header
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	spans := exporter.GetSpans()
	handlerStub, found := findStubByName(spans, "handler.GetSubscription")
	require.True(t, found)

	assert.False(t, handlerStub.Parent.IsValid(), "handler span must be root span when parent context is absent")

	serviceStub, found := findStubByName(spans, "SubscriptionService.GetDetail")
	require.True(t, found)
	assert.Equal(t, handlerStub.SpanContext.TraceID(), serviceStub.SpanContext.TraceID())
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
	r.POST("/subscriptions/:id/status", func(c *gin.Context) {
		c.Set("tenantID", "tenant-1")
		c.Set("callerID", "caller-1")
	}, NewChangeSubscriptionStatusHandler(svc))

	body, _ := json.Marshal(map[string]string{"status": "cancelled"})
	req := httptest.NewRequest(http.MethodPost, "/subscriptions/sub-2/status", bytes.NewReader(body))
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
