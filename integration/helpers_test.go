//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellabill-backend/internal/handlers"
	"stellabill-backend/internal/middleware"
	"stellabill-backend/internal/repository"
	repopostgres "stellabill-backend/internal/repository/postgres"
	"stellabill-backend/internal/service"
)

const testJWTSecret = "integration-test-jwt-secret-32ch!!"

// makeTestJWT generates a signed HS256 JWT with the given subject and a 1-hour expiry.
func makeTestJWT(subject string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": subject,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		panic("makeTestJWT: " + err.Error())
	}
	return signed
}

// buildRouter wires the real Postgres repositories into a gin router that
// mirrors the production route layout used by routes.Register.
func buildRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	subRepo := repopostgres.NewSubscriptionRepo(pool)
	planRepo := repopostgres.NewPlanRepo(pool)
	svc := service.NewSubscriptionService(subRepo, planRepo)

	api := r.Group("/api")
	api.GET("/health", handlers.Health)
	api.GET("/subscriptions", handlers.ListSubscriptions)
	api.GET("/subscriptions/:id",
		middleware.AuthMiddleware(testJWTSecret),
		handlers.NewGetSubscriptionHandler(svc),
	)
	api.GET("/plans", handlers.ListPlans)

	return r
}

// do executes a request against the given router and returns the recorded response.
// token may be empty, in which case no Authorization header is sent.
func do(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

// seedPlan inserts a PlanRow into the database and registers a t.Cleanup
// function that deletes it afterwards (best-effort).
func seedPlan(t *testing.T, pool *pgxpool.Pool, p *repository.PlanRow) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO plans (id, name, amount, currency, interval, description)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		p.ID, p.Name, p.Amount, p.Currency, p.Interval, p.Description,
	)
	if err != nil {
		t.Fatalf("seedPlan %q: %v", p.ID, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM plans WHERE id = $1`, p.ID); err != nil {
			t.Logf("cleanup: delete plan %q: %v", p.ID, err)
		}
	})
}

// seedSubscription inserts a SubscriptionRow into the database and registers a
// t.Cleanup function that deletes it afterwards (best-effort).
func seedSubscription(t *testing.T, pool *pgxpool.Pool, s *repository.SubscriptionRow) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO subscriptions
		   (id, plan_id, customer_id, status, amount, currency, interval, next_billing, deleted_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		s.ID, s.PlanID, s.CustomerID, s.Status,
		s.Amount, s.Currency, s.Interval, s.NextBilling, s.DeletedAt,
	)
	if err != nil {
		t.Fatalf("seedSubscription %q: %v", s.ID, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM subscriptions WHERE id = $1`, s.ID); err != nil {
			t.Logf("cleanup: delete subscription %q: %v", s.ID, err)
		}
	})
}

// uniqueID generates a test-local unique string ID by combining a prefix with
// the test name and a suffix, keeping IDs deterministic and readable in logs.
func uniqueID(prefix string, t *testing.T, suffix string) string {
	return fmt.Sprintf("%s-%s-%s", prefix, t.Name(), suffix)
}

