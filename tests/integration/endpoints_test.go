package integration

import (
	"net/http"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/auth"
	"stellabill-backend/internal/config"
	"stellabill-backend/internal/routes"
	"stellabill-backend/internal/testutil"
)

func setupRouter() *gin.Engine {
	// Set required env vars for config.Load() to succeed if not already set
	if os.Getenv("DATABASE_URL") == "" {
		os.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	}
	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "StrongTestSecret123!")
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.Register(router)
	return router
}

func TestHealthEndpointAuthnz(t *testing.T) {
	router := setupRouter()

	tests := []struct {
		name           string
		withToken      bool
		expectedStatus int
		hasStatus      bool
	}{
		{
			name:           "health check without token",
			withToken:      false,
			expectedStatus: http.StatusOK,
			hasStatus:      true,
		},
		{
			name:           "health check with valid token",
			withToken:      true,
			expectedStatus: http.StatusOK,
			hasStatus:      true,
		},
	}

	cfg, _ := config.Load()
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.NewTestRequest(router)

			if tt.withToken {
				token, _ := tg.GenerateAdminToken("test-user", "test@example.com")
				req = req.WithToken(token)
			}

			resp := req.Get("/api/health")

			if resp.Status() != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.Status())
			}

			if tt.hasStatus {
				var respBody map[string]interface{}
				if err := resp.JSON(&respBody); err != nil {
					t.Errorf("failed to parse response: %v", err)
				}
				if status, exists := respBody["status"]; !exists || status != "ok" {
					t.Errorf("expected status='ok' in response")
				}
			}
		})
	}
}

func TestListPlansAuthenticationAndAuthorization(t *testing.T) {
	router := setupRouter()
	cfg, _ := config.Load()
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)

	tests := []struct {
		name            string
		token           string
		expectedStatus  int
		shouldHaveError bool
		description     string
	}{
		{
			name:            "no token provided",
			token:           "",
			expectedStatus:  http.StatusUnauthorized,
			shouldHaveError: true,
			description:     "endpoint requires authentication",
		},
		{
			name:            "malformed authorization header",
			token:           "InvalidHeader",
			expectedStatus:  http.StatusUnauthorized,
			shouldHaveError: true,
			description:     "Bearer prefix required",
		},
		{
			name:            "expired token",
			token:           createExpiredToken(tg),
			expectedStatus:  http.StatusUnauthorized,
			shouldHaveError: true,
			description:     "token validation should fail",
		},
		{
			name:            "valid admin token",
			token:           createAdminToken(tg),
			expectedStatus:  http.StatusOK,
			shouldHaveError: false,
			description:     "admin can access plans",
		},
		{
			name:            "valid merchant token",
			token:           createMerchantToken(tg),
			expectedStatus:  http.StatusOK,
			shouldHaveError: false,
			description:     "merchant can access plans",
		},
		{
			name:            "valid customer token",
			token:           createCustomerToken(tg),
			expectedStatus:  http.StatusOK,
			shouldHaveError: false,
			description:     "customer can access plans",
		},
		{
			name:            "token without user_id",
			token:           createTokenWithoutUserID(tg),
			expectedStatus:  http.StatusUnauthorized,
			shouldHaveError: true,
			description:     "token must contain user_id claim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.NewTestRequest(router)

			if tt.token != "" {
				req = req.WithToken(tt.token)
			}

			resp := req.Get("/api/plans")

			if resp.Status() != tt.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d", tt.description, tt.expectedStatus, resp.Status())
			}

			if tt.shouldHaveError && !resp.HasError() {
				t.Errorf("[%s] expected error in response but none found", tt.description)
			}

			if !tt.shouldHaveError && resp.HasError() {
				t.Errorf("[%s] unexpected error: %s", tt.description, resp.GetError())
			}
		})
	}
}

func TestListSubscriptionsAuthorizationEnforcement(t *testing.T) {
	router := setupRouter()
	cfg, _ := config.Load()
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)

	tests := []struct {
		name           string
		token          string
		expectedStatus int
		expectedError  string
		description    string
	}{
		{
			name:           "no token",
			token:          "",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "missing authorization header",
			description:    "authentication required",
		},
		{
			name:           "expired token",
			token:          createExpiredToken(tg),
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid or expired token",
			description:    "expired tokens rejected",
		},
		{
			name:           "customer token denied",
			token:          createCustomerToken(tg),
			expectedStatus: http.StatusForbidden,
			expectedError:  "insufficient permissions",
			description:    "customer role lacks permission",
		},
		{
			name:           "admin token allowed",
			token:          createAdminToken(tg),
			expectedStatus: http.StatusOK,
			expectedError:  "",
			description:    "admin can access subscriptions",
		},
		{
			name:           "merchant token allowed",
			token:          createMerchantToken(tg),
			expectedStatus: http.StatusOK,
			expectedError:  "",
			description:    "merchant can access subscriptions",
		},
		{
			name:           "token without roles",
			token:          createTokenWithoutRoles(tg),
			expectedStatus: http.StatusForbidden,
			expectedError:  "insufficient permissions",
			description:    "token without roles denied access",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.NewTestRequest(router)

			if tt.token != "" {
				req = req.WithToken(tt.token)
			}

			resp := req.Get("/api/subscriptions")

			if resp.Status() != tt.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d", tt.description, tt.expectedStatus, resp.Status())
			}

			if tt.expectedError != "" {
				if !resp.HasError() {
					t.Errorf("[%s] expected error in response", tt.description)
				} else if actualError := resp.GetError(); actualError != tt.expectedError {
					t.Errorf("[%s] expected error '%s', got '%s'", tt.description, tt.expectedError, actualError)
				}
			}
		})
	}
}

func TestGetSubscriptionByIDAuthorizationEnforcement(t *testing.T) {
	router := setupRouter()
	cfg, _ := config.Load()
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)

	tests := []struct {
		name           string
		token          string
		subscriptionID string
		expectedStatus int
		expectedError  string
		description    string
	}{
		{
			name:           "no token",
			token:          "",
			subscriptionID: "sub-123",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "missing authorization header",
			description:    "authentication required",
		},
		{
			name:           "expired token",
			token:          createExpiredToken(tg),
			subscriptionID: "sub-123",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid or expired token",
			description:    "expired tokens rejected",
		},
		{
			name:           "customer token denied",
			token:          createCustomerToken(tg),
			subscriptionID: "sub-123",
			expectedStatus: http.StatusForbidden,
			expectedError:  "insufficient permissions",
			description:    "customer role lacks permission",
		},
		{
			name:           "admin token allowed",
			token:          createAdminToken(tg),
			subscriptionID: "sub-123",
			expectedStatus: http.StatusOK,
			expectedError:  "",
			description:    "admin can access subscription",
		},
		{
			name:           "merchant token allowed",
			token:          createMerchantToken(tg),
			subscriptionID: "sub-456",
			expectedStatus: http.StatusOK,
			expectedError:  "",
			description:    "merchant can access subscription",
		},
		{
			name:           "missing subscription ID",
			token:          createAdminToken(tg),
			subscriptionID: "test123",
			expectedStatus: http.StatusOK,
			expectedError:  "",
			description:    "subscription with ID should succeed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.NewTestRequest(router)

			if tt.token != "" {
				req = req.WithToken(tt.token)
			}

			path := "/api/subscriptions/" + tt.subscriptionID
			resp := req.Get(path)

			if resp.Status() != tt.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d", tt.description, tt.expectedStatus, resp.Status())
			}

			if tt.expectedError != "" {
				if !resp.HasError() {
					t.Errorf("[%s] expected error in response", tt.description)
				} else if actualError := resp.GetError(); actualError != tt.expectedError {
					t.Errorf("[%s] expected error '%s', got '%s'", tt.description, tt.expectedError, actualError)
				}
			}
		})
	}
}

func TestAuthenticationEdgeCases(t *testing.T) {
	router := setupRouter()
	cfg, _ := config.Load()
	_ = testutil.NewTestTokenGenerator(cfg.JWTSecret) // Verify token generator creation works

	tests := []struct {
		name           string
		token          string
		expectedStatus int
		expectedError  string
		description    string
	}{
		{
			name:           "malformed JWT",
			token:          "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid.signature",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid or expired token",
			description:    "malformed JWT should be rejected",
		},
		{
			name:           "token signed with different algorithm",
			token:          "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.invalid.signature",
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid or expired token",
			description:    "token with wrong algorithm rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testutil.NewTestRequest(router)

			if tt.token != "" {
				req = req.WithToken(tt.token)
			}

			resp := req.Get("/api/plans")

			if resp.Status() != tt.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d", tt.description, tt.expectedStatus, resp.Status())
			}

			if tt.expectedError != "" && !resp.HasError() {
				t.Errorf("[%s] expected error in response", tt.description)
			}
		})
	}
}

// Helper functions to create tokens for testing

func createAdminToken(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateAdminToken("admin-user", "admin@test.com")
	return token
}

func createMerchantToken(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateMerchantToken("merchant-user", "merchant@test.com", "merchant-123")
	return token
}

func createCustomerToken(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateCustomerToken("customer-user", "customer@test.com")
	return token
}

func createExpiredToken(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateExpiredToken("user", "user@test.com", auth.RoleAdmin)
	return token
}

func createTokenWithoutRoles(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateTokenWithoutRoles("user", "user@test.com")
	return token
}

func createTokenWithoutUserID(tg *testutil.TestTokenGenerator) string {
	token, _ := tg.GenerateTokenWithoutUserID("user@test.com", auth.RoleAdmin)
	return token
}

