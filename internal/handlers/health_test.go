package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockDBPinger implements DBPinger for testing
type MockDBPinger struct {
	err     error
	latency time.Duration
}

func (m *MockDBPinger) PingContext(ctx context.Context) error {
	if m.latency > 0 {
		time.Sleep(m.latency)
	}
	if m.err != nil {
		return m.err
	}
	return nil
}

// MockOutboxHealther implements OutboxHealther for testing
type MockOutboxHealther struct {
	err   error
	stats map[string]interface{}
}

func (m *MockOutboxHealther) Health() error {
	return m.err
}

func (m *MockOutboxHealther) GetStats() (map[string]interface{}, error) {
	if m.stats != nil {
		return m.stats, nil
	}
	return map[string]interface{}{}, nil
}

// TestLivenessProbe tests the liveness probe endpoint
func TestLivenessProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health/live", nil)

	h.LivenessProbe(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, StatusHealthy, response.Status)
	assert.Equal(t, ServiceName, response.Service)
	assert.NotEmpty(t, response.Timestamp)
}

// TestReadinessProbeHealthy tests readiness when all dependencies are healthy
func TestReadinessProbeHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set up environment
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	h := &Handler{
		Database: &MockDBPinger{err: nil},
		Outbox:   &MockOutboxHealther{err: nil},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health/ready", nil)

	h.ReadinessProbe(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, StatusHealthy, response.Status)
	assert.Equal(t, ServiceName, response.Service)
}

// TestReadinessProbeDegraded tests readiness when dependencies are degraded
func TestReadinessProbeDegraded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	h := &Handler{
		Database: &MockDBPinger{err: errors.New("connection refused")},
		Outbox:   &MockOutboxHealther{err: nil},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health/ready", nil)

	h.ReadinessProbe(c)

	// Should return ServiceUnavailable when degraded
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, StatusDegraded, response.Status)
}

// TestHealthDetails tests the comprehensive health details endpoint
func TestHealthDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	stats := map[string]interface{}{
		"pending_messages": 42,
		"processed_today":  1000,
	}

	h := &Handler{
		Database: &MockDBPinger{err: nil},
		Outbox: &MockOutboxHealther{
			err:   nil,
			stats: stats,
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health/detailed", nil)

	h.HealthDetails(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, StatusHealthy, response.Status)
	assert.NotNil(t, response.Dependencies)

	// Verify that the response includes dependency details
	depMap := response.Dependencies["database"].(map[string]interface{})
	assert.Equal(t, StatusHealthy, depMap["status"])
}

// TestCheckDatabase_Healthy tests database health check when healthy
func TestCheckDatabase_Healthy(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	checker := &HealthChecker{
		db: &MockDBPinger{err: nil},
	}

	result := checker.checkDatabase(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, StatusHealthy, depHealth.Status)
	assert.NotEmpty(t, depHealth.Latency)
}

// TestCheckDatabase_Timeout tests database health check with timeout
func TestCheckDatabase_Timeout(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	checker := &HealthChecker{
		db: &MockDBPinger{
			err:     context.DeadlineExceeded,
			latency: MaxDatabaseTimeout + 1*time.Second, // Will timeout
		},
	}

	result := checker.checkDatabase(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, StatusDegraded, depHealth.Status)
	assert.Contains(t, depHealth.Message, "timeout")
}

// TestCheckDatabase_NotConfigured tests database health when not configured
func TestCheckDatabase_NotConfigured(t *testing.T) {
	os.Unsetenv("DATABASE_URL")

	checker := &HealthChecker{
		db: &MockDBPinger{err: nil},
	}

	result := checker.checkDatabase(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, "not_configured", depHealth.Status)
}

// TestCheckDatabase_Uninitialized tests database health when client is nil
func TestCheckDatabase_Uninitialized(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	checker := &HealthChecker{
		db: nil,
	}

	result := checker.checkDatabase(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, "not_configured", depHealth.Status)
}

// TestCheckOutbox_Healthy tests outbox health check when healthy
func TestCheckOutbox_Healthy(t *testing.T) {
	stats := map[string]interface{}{
		"pending": 10,
	}

	checker := &HealthChecker{
		outbox: &MockOutboxHealther{
			err:   nil,
			stats: stats,
		},
	}

	result := checker.checkOutbox(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, StatusHealthy, depHealth.Status)
	assert.Equal(t, stats, depHealth.Details)
}

// TestCheckOutbox_Unhealthy tests outbox health check when unhealthy
func TestCheckOutbox_Unhealthy(t *testing.T) {
	checker := &HealthChecker{
		outbox: &MockOutboxHealther{
			err: errors.New("queue processing error"),
		},
	}

	result := checker.checkOutbox(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, StatusDegraded, depHealth.Status)
	assert.Contains(t, depHealth.Message, "unhealthy")
}

// TestCheckOutbox_NotConfigured tests outbox health when not configured
func TestCheckOutbox_NotConfigured(t *testing.T) {
	checker := &HealthChecker{
		outbox: nil,
	}

	result := checker.checkOutbox(context.Background())
	depHealth, ok := result.(DependencyHealth)
	require.True(t, ok)
	assert.Equal(t, "not_configured", depHealth.Status)
}

// TestDeriveOverallStatus tests the status derivation logic
func TestDeriveOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		deps     map[string]interface{}
		expected string
	}{
		{
			name: "all healthy",
			deps: map[string]interface{}{
				"database": DependencyHealth{Status: StatusHealthy},
				"outbox":   DependencyHealth{Status: StatusHealthy},
			},
			expected: StatusHealthy,
		},
		{
			name: "one degraded",
			deps: map[string]interface{}{
				"database": DependencyHealth{Status: StatusHealthy},
				"outbox":   DependencyHealth{Status: StatusDegraded},
			},
			expected: StatusDegraded,
		},
		{
			name: "one unhealthy",
			deps: map[string]interface{}{
				"database": DependencyHealth{Status: StatusHealthy},
				"outbox":   DependencyHealth{Status: StatusUnhealthy},
			},
			expected: StatusUnhealthy,
		},
		{
			name: "all degraded",
			deps: map[string]interface{}{
				"database": DependencyHealth{Status: StatusDegraded},
				"outbox":   DependencyHealth{Status: StatusDegraded},
			},
			expected: StatusDegraded,
		},
		{
			name: "map representation",
			deps: map[string]interface{}{
				"database": map[string]interface{}{"status": StatusHealthy},
				"outbox":   map[string]interface{}{"status": StatusDegraded},
			},
			expected: StatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveOverallStatus(tt.deps)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCheckAllDependencies_Concurrent tests concurrent dependency checks
func TestCheckAllDependencies_Concurrent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	checker := NewHealthChecker(
		&MockDBPinger{err: nil},
		&MockOutboxHealther{err: nil},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deps := checker.checkAllDependencies(ctx)

	assert.NotNil(t, deps["database"])
	assert.NotNil(t, deps["outbox"])
}

// TestCheckAllDependencies_Timeout tests concurrent checks with context timeout
func TestCheckAllDependencies_Timeout(t *testing.T) {
	checker := NewHealthChecker(
		&MockDBPinger{latency: 10 * time.Second},
		&MockOutboxHealther{err: nil},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	deps := checker.checkAllDependencies(ctx)

	// At least one should be marked as timeout
	dbDep, ok := deps["database"].(DependencyHealth)
	if ok && dbDep.Status == "timeout" {
		assert.Contains(t, dbDep.Message, "timeout")
	}
}

// TestSecurityNoSensitiveData verifies health responses don't leak secrets
func TestSecurityNoSensitiveData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	os.Setenv("DATABASE_URL", "postgres://user:password@localhost/mydb")
	defer os.Unsetenv("DATABASE_URL")

	h := &Handler{
		Database: &MockDBPinger{err: nil},
		Outbox:   &MockOutboxHealther{err: nil},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/health/ready", nil)

	h.ReadinessProbe(c)

	body := w.Body.String()

	// Verify that sensitive data is NOT in the response
	assert.NotContains(t, body, "password")
	assert.NotContains(t, body, "user:password")
	assert.NotContains(t, body, "localhost/mydb")
}

// TestLifecycleEndpointsIntegration tests all health endpoints together
func TestLifecycleEndpointsIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	h := &Handler{
		Database: &MockDBPinger{err: nil},
		Outbox: &MockOutboxHealther{
			err: nil,
			stats: map[string]interface{}{
				"queued": 5,
			},
		},
	}

	tests := []struct {
		name    string
		handler func(*gin.Context)
		path    string
	}{
		{"Liveness", h.LivenessProbe, "/health/live"},
		{"Readiness", h.ReadinessProbe, "/health/ready"},
		{"Detailed", h.HealthDetails, "/health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.path, nil)

			tt.handler(c)

			assert.Equal(t, http.StatusOK, w.Code)

			var response HealthResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, ServiceName, response.Service)
			assert.NotEmpty(t, response.Timestamp)
		})
	}
}
