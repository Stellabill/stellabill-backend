package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

const testJWTSecret = "dev-secret"

func makeTestJWT(subject, tenant string, roles []string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    subject,
		"tenant": tenant,
		"roles":  roles,
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		panic("failed to sign test JWT: " + err.Error())
	}
	return signed
}

func buildIntegrationRouter(stmtRepo repository.StatementRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := service.NewStatementService(nil, stmtRepo)
	
	r.GET("/api/statements/:id",
		middleware.AuthMiddleware(testJWTSecret),
		auth.RequirePermission(auth.PermReadStatements),
		handlers.NewGetStatementHandler(svc),
	)
	r.GET("/api/statements",
		middleware.AuthMiddleware(testJWTSecret),
		auth.RequirePermission(auth.PermReadStatements),
		handlers.NewListStatementsHandler(svc),
	)
	return r
}

func TestIntegration_StatementRBAC(t *testing.T) {
	stmtRepo := repository.NewMockStatementRepo(&repository.StatementRow{
		ID:             "stmt-1",
		CustomerID:     "cust-1",
		SubscriptionID: "sub-1",
		TotalAmount:    "100",
		Currency:       "USD",
		Status:         "paid",
	})
	r := buildIntegrationRouter(stmtRepo)

	t.Run("customer access own statement", func(t *testing.T) {
		token := makeTestJWT("cust-1", "tenant-1", []string{"customer"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("customer access other statement returns 403", func(t *testing.T) {
		token := makeTestJWT("cust-2", "tenant-1", []string{"customer"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("admin access any statement", func(t *testing.T) {
		token := makeTestJWT("admin-1", "tenant-1", []string{"admin"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("merchant access any statement", func(t *testing.T) {
		token := makeTestJWT("merch-1", "tenant-1", []string{"merchant"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestIntegration_StatementPagination(t *testing.T) {
	rows := make([]*repository.StatementRow, 0)
	for i := 1; i <= 5; i++ {
		rows = append(rows, &repository.StatementRow{
			ID:         "stmt-" + string(rune('0'+i)),
			CustomerID: "cust-1",
			IssuedAt:   time.Now().Format(time.RFC3339),
		})
	}
	stmtRepo := repository.NewMockStatementRepo(rows...)
	r := buildIntegrationRouter(stmtRepo)
	token := makeTestJWT("cust-1", "tenant-1", []string{"customer"})

	t.Run("limit works", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/statements?limit=2", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		stmts := data["statements"].([]interface{})
		if len(stmts) != 2 {
			t.Errorf("expected 2 statements, got %d", len(stmts))
		}
		pag := resp["pagination"].(map[string]interface{})
		if pag["has_more"] != true {
			t.Errorf("expected has_more=true")
		}
		if pag["next_cursor"] == "" {
			t.Errorf("expected next_cursor to be set")
		}
	})
}
