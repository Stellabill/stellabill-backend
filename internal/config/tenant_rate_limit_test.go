package config

import (
	"os"
	"strings"
	"testing"
)

// TestRateLimitTenantDisabledByDefault verifies per-tenant rate limiting is
// off unless explicitly configured.
func TestRateLimitTenantDisabledByDefault(t *testing.T) {
	c := &Config{}
	_ = c.validate(map[string]string{}, nil)

	if c.RateLimitTenantRPS != 0 {
		t.Fatalf("expected RateLimitTenantRPS to default to 0 (disabled), got %d", c.RateLimitTenantRPS)
	}
	if c.RateLimitTenantBurst != 0 {
		t.Fatalf("expected RateLimitTenantBurst to default to 0, got %d", c.RateLimitTenantBurst)
	}
}

func TestRateLimitTenantValidConfig(t *testing.T) {
	os.Setenv("RATE_LIMIT_TENANT_RPS", "50")
	defer os.Unsetenv("RATE_LIMIT_TENANT_RPS")
	os.Setenv("RATE_LIMIT_TENANT_BURST", "100")
	defer os.Unsetenv("RATE_LIMIT_TENANT_BURST")

	c := &Config{}
	result := c.validate(map[string]string{}, nil)

	if !result.Valid() {
		t.Fatalf("expected valid result, got: %s", result.Error())
	}
	if c.RateLimitTenantRPS != 50 {
		t.Fatalf("expected RateLimitTenantRPS 50, got %d", c.RateLimitTenantRPS)
	}
	if c.RateLimitTenantBurst != 100 {
		t.Fatalf("expected RateLimitTenantBurst 100, got %d", c.RateLimitTenantBurst)
	}
}

// TestRateLimitTenantBurstDefaultsTo2xRPS verifies that setting only
// RATE_LIMIT_TENANT_RPS derives a conservative burst of 2x RPS.
func TestRateLimitTenantBurstDefaultsTo2xRPS(t *testing.T) {
	os.Setenv("RATE_LIMIT_TENANT_RPS", "25")
	defer os.Unsetenv("RATE_LIMIT_TENANT_RPS")

	c := &Config{}
	result := c.validate(map[string]string{}, nil)

	if !result.Valid() {
		t.Fatalf("expected valid result, got: %s", result.Error())
	}
	if c.RateLimitTenantRPS != 25 {
		t.Fatalf("expected RateLimitTenantRPS 25, got %d", c.RateLimitTenantRPS)
	}
	if c.RateLimitTenantBurst != 50 {
		t.Fatalf("expected burst to default to 2x RPS (50), got %d", c.RateLimitTenantBurst)
	}
}

func TestRateLimitTenantInvalidValues(t *testing.T) {
	testCases := []struct {
		name  string
		rps   string
		burst string
	}{
		{"rps out of range", "9999999", ""},
		{"rps non-numeric", "not-a-number", ""},
		{"burst out of range", "50", "9999999"},
		{"burst non-numeric", "50", "abc"},
		{"burst below rps", "50", "20"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("RATE_LIMIT_TENANT_RPS", tc.rps)
			defer os.Unsetenv("RATE_LIMIT_TENANT_RPS")
			os.Setenv("RATE_LIMIT_TENANT_BURST", tc.burst)
			defer os.Unsetenv("RATE_LIMIT_TENANT_BURST")

			c := &Config{}
			result := c.validate(map[string]string{}, nil)

			found := false
			for _, e := range result.Errors {
				if strings.HasPrefix(e.Key, "RATE_LIMIT_TENANT_") {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected a RATE_LIMIT_TENANT_* validation error, got: %s", result.Error())
			}
		})
	}
}
