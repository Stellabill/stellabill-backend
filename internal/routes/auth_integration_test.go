package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"stellarbill-backend/internal/auth"
)

func setupTestRouter() (*gin.Engine, string) {
	gin.SetMode(gin.TestMode)

	secret := "Test-Secret-123!"
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("JWT_SECRET", secret)
	os.Setenv("ADMIN_TOKEN", "Another-Strong-Admin-Token-456!")

	r := gin.New()
	Register(r)

	return r, secret
}

func createToken(secret string, sub string, roles []auth.Role, exp time.Time) (string, error) {
	claims := jwt.MapClaims{
		"sub":       sub,
		"roles":     roles,
		"exp":       exp.Unix(),
		"iat":       time.Now().Unix(),
		"tenant_id": "tenant-1",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func TestAuthMiddleware_Integration(t *testing.T) {
	r, secret := setupTestRouter()
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_TOKEN")
	}()

	tests := []struct {
		name           string
		method         string
		url            string
		token          string
		headers        map[string]string
		expectedStatus int
	}{
		// Unauthenticated
		{"Unauthenticated GET /api/v1/subscriptions", http.MethodGet, "/api/v1/subscriptions", "", nil, http.StatusUnauthorized},
		{"Unauthenticated GET /api/v1/subscriptions/sub-123", http.MethodGet, "/api/v1/subscriptions/sub-123", "", nil, http.StatusUnauthorized},
		{"Unauthenticated POST /api/admin/purge", http.MethodPost, "/api/admin/purge", "", nil, http.StatusUnauthorized},

		// Invalid Token
		{"Invalid Token GET /api/v1/subscriptions", http.MethodGet, "/api/v1/subscriptions", "invalid-token", nil, http.StatusUnauthorized},

		// Expired Token
		{"Expired Token GET /api/v1/subscriptions", http.MethodGet, "/api/v1/subscriptions", func() string {
			tok, _ := createToken(secret, "user-1", []auth.Role{auth.RoleUser}, time.Now().Add(-time.Hour))
			return tok
		}(), nil, http.StatusUnauthorized},

		// Forbidden (Insufficient Role)
		{"Forbidden POST /api/admin/purge (User role)", http.MethodPost, "/api/admin/purge", func() string {
			tok, _ := createToken(secret, "user-1", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
			return tok
		}(), map[string]string{"Idempotency-Key": "test-key", "X-Admin-Token": "Another-Strong-Admin-Token-456!"}, http.StatusForbidden},

		{"Forbidden GET /api/subscriptions (Customer role)", http.MethodGet, "/api/subscriptions", func() string {
			tok, _ := createToken(secret, "user-1", []auth.Role{auth.RoleCustomer}, time.Now().Add(time.Hour))
			return tok
		}(), nil, http.StatusForbidden},

		// Permitted (Success)
		{"Permitted GET /api/v1/subscriptions (User role)", http.MethodGet, "/api/v1/subscriptions", func() string {
			tok, _ := createToken(secret, "user-1", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
			return tok
		}(), nil, http.StatusOK},

		{"Permitted GET /api/v1/subscriptions/sub-123 (User role)", http.MethodGet, "/api/v1/subscriptions/sub-123", func() string {
			tok, _ := createToken(secret, "user-1", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
			return tok
		}(), nil, http.StatusOK},

		{"Permitted POST /api/admin/purge (Admin role)", http.MethodPost, "/api/admin/purge", func() string {
			tok, _ := createToken(secret, "admin-1", []auth.Role{auth.RoleAdmin}, time.Now().Add(time.Hour))
			return tok
		}(), map[string]string{"Idempotency-Key": "test-key", "X-Admin-Token": "Another-Strong-Admin-Token-456!"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.url, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			r.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}
