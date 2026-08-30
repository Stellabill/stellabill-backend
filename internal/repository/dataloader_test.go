package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDataLoaderMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	planRepo := NewMockPlanRepo()
	subRepo := NewMockSubscriptionRepo()

	r := gin.New()
	r.Use(DataLoaderMiddleware(planRepo, subRepo))

	var retrievedFromGinContext bool
	var retrievedFromStdContext bool

	r.GET("/test-loader", func(c *gin.Context) {
		loader1 := GetDataLoader(c)
		if loader1 != nil {
			retrievedFromGinContext = true
		}

		loader2 := LoaderFromContext(c.Request.Context())
		if loader2 != nil {
			retrievedFromStdContext = true
		}

		if loader1 != loader2 {
			c.String(http.StatusInternalServerError, "loader mismatch")
			return
		}

		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test-loader", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", w.Code)
	}

	if !retrievedFromGinContext {
		t.Fatalf("failed to retrieve loader from Gin context")
	}

	if !retrievedFromStdContext {
		t.Fatalf("failed to retrieve loader from std context")
	}
}
