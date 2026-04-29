package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

const testJWTSecret = "integration-test-secret"

// testAuthMiddleware is a minimal JWT auth middleware for integration tests.
// It validates the Authorization header, parses tenant from the JWT claim
// and/or X-Tenant-ID header, and sets callerID and tenantID in the context.
func testAuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: "authorization header required",
				TraceID: uuid.New().String(),
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: "authorization header must be Bearer token",
				TraceID: uuid.New().String(),
			})
			return
		}

		tokenStr := parts[1]
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}))

		if err != nil || !token.Valid {
			msg := "invalid or expired token"
			if err != nil {
				msg = fmt.Sprintf("token validation failed: %v", err)
			}
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: msg,
				TraceID: uuid.New().String(),
			})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: "invalid token claims",
				TraceID: uuid.New().String(),
			})
			return
		}

		sub, err := claims.GetSubject()
		if err != nil || sub == "" {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: "token missing subject claim",
				TraceID: uuid.New().String(),
			})
			return
		}

		tenantHeader := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		tenantClaim := ""
		if v, ok := claims["tenant"]; ok {
			if ts, ok := v.(string); ok {
				tenantClaim = strings.TrimSpace(ts)
			}
		}

		var tenantID string
		if tenantHeader != "" && tenantClaim != "" {
			if tenantHeader != tenantClaim {
				c.Header("Content-Type", "application/json; charset=utf-8")
				c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
					Code:    string(ErrorCodeUnauthorized),
					Message: "tenant mismatch",
					TraceID: uuid.New().String(),
				})
				return
			}
			tenantID = tenantHeader
		} else if tenantHeader != "" {
			tenantID = tenantHeader
		} else if tenantClaim != "" {
			tenantID = tenantClaim
		} else {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorEnvelope{
				Code:    string(ErrorCodeUnauthorized),
				Message: "tenant id required",
				TraceID: uuid.New().String(),
			})
			return
		}

		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)
		c.Next()
	}
}

// makeTestJWT generates a valid HS256 JWT with the given subject and tenant, signed with testJWTSecret.
func makeTestJWT(subject, tenant string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    subject,
		"tenant": tenant,
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		panic("failed to sign test JWT: " + err.Error())
	}
	return signed
}

// buildIntegrationRouter wires AuthMiddleware + real SubscriptionService backed by mock repos.
func buildIntegrationRouter(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := service.NewSubscriptionService(subRepo, planRepo)
	r.GET("/api/subscriptions/:id",
		testAuthMiddleware(testJWTSecret),
		NewGetSubscriptionHandler(svc),
	)
	return r
}

func TestIntegration_GetSubscription_HappyPath(t *testing.T) {
	const customerID = "cust-abc"
	const subID = "sub-xyz"
	const planID = "plan-pro"

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:          subID,
		PlanID:      planID,
		TenantID:    "tenant-1",
		CustomerID:  customerID,
		Status:      "active",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "monthly",
		NextBilling: "2024-03-01T00:00:00Z",
		DeletedAt:   nil,
	})
	planRepo := repository.NewMockPlanRepo(&repository.PlanRow{
		ID:          planID,
		Name:        "Pro Plan",
		Amount:      "2999",
		Currency:    "USD",
		Interval:    "monthly",
		Description: "The professional tier",
	})

	r := buildIntegrationRouter(subRepo, planRepo)

	// 1. Generate a valid JWT whose subject is the customer ID and tenant.
	tokenStr := makeTestJWT(customerID, "tenant-1")

	// 2. Make the GET request with Authorization + tenant headers.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	// 3. Assert HTTP 200.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// 4. Assert Content-Type.
	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json; charset=utf-8, got %q", ct)
	}

	// 5. Decode and assert full ResponseEnvelope shape.
	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// api_version = "1"
	if envelope["api_version"] != "1" {
		t.Errorf("expected api_version=1, got %v", envelope["api_version"])
	}

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be an object, got %T", envelope["data"])
	}

	// data.id
	if data["id"] != subID {
		t.Errorf("expected data.id=%q, got %v", subID, data["id"])
	}
	// data.plan_id
	if data["plan_id"] != planID {
		t.Errorf("expected data.plan_id=%q, got %v", planID, data["plan_id"])
	}
	// data.customer
	if data["customer"] != customerID {
		t.Errorf("expected data.customer=%q, got %v", customerID, data["customer"])
	}
	// data.status
	if data["status"] != "active" {
		t.Errorf("expected data.status=active, got %v", data["status"])
	}
	// data.interval
	if data["interval"] != "monthly" {
		t.Errorf("expected data.interval=monthly, got %v", data["interval"])
	}

	// data.plan (non-nil)
	plan, ok := data["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data.plan to be an object, got %T", data["plan"])
	}
	if plan["plan_id"] != planID {
		t.Errorf("expected plan.plan_id=%q, got %v", planID, plan["plan_id"])
	}
	if plan["name"] != "Pro Plan" {
		t.Errorf("expected plan.name=Pro Plan, got %v", plan["name"])
	}

	// data.billing_summary
	billing, ok := data["billing_summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data.billing_summary to be an object, got %T", data["billing_summary"])
	}
	if billing["amount_cents"] != float64(2999) {
		t.Errorf("expected billing_summary.amount_cents=2999, got %v", billing["amount_cents"])
	}
	if billing["currency"] != "USD" {
		t.Errorf("expected billing_summary.currency=USD, got %v", billing["currency"])
	}
}

func TestIntegration_GetSubscription_MissingTenant_Returns401(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	tokenStr := makeTestJWT("cust-1", "")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	// no X-Tenant-ID header and no tenant claim
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestIntegration_GetSubscription_SpoofedTenant_Returns401(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{ID: "sub-1", TenantID: "tenant-1", CustomerID: "cust-1"})
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	tokenStr := makeTestJWT("cust-1", "tenant-1")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-2")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestIntegration_GetSubscription_MissingAuthHeader_Returns401(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestIntegration_GetSubscription_InvalidToken_Returns401(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestIntegration_GetSubscription_WrongCaller_Returns403(t *testing.T) {
	const ownerID = "owner-1"
	const subID = "sub-1"

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:         subID,
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: ownerID,
		Status:     "active",
		Amount:     "999",
		Currency:   "USD",
		Interval:   "monthly",
	})
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	// JWT subject is a different caller but same tenant.
	tokenStr := makeTestJWT("other-caller", "tenant-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIntegration_GetSubscription_SoftDeleted_Returns410(t *testing.T) {
	const customerID = "cust-del"
	const subID = "sub-del"
	now := time.Now()

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:         subID,
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: customerID,
		Status:     "cancelled",
		Amount:     "999",
		Currency:   "USD",
		Interval:   "monthly",
		DeletedAt:  &now,
	})
	planRepo := repository.NewMockPlanRepo()
	r := buildIntegrationRouter(subRepo, planRepo)

	tokenStr := makeTestJWT(customerID, "tenant-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
	var body ErrorEnvelope
	json.NewDecoder(w.Body).Decode(&body)
	if body.Code != string(ErrorCodeNotFound) {
		t.Errorf("unexpected error code: %q", body.Code)
	}
}
