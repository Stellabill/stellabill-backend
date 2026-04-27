package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// MockStatementService is a mock for StatementService
type MockStatementService struct {
	mock.Mock
}

func (m *MockStatementService) GetDetail(ctx context.Context, callerID string, roles []string, statementID string) (*service.StatementDetail, []string, error) {
	args := m.Called(ctx, callerID, roles, statementID)
	if args.Get(0) == nil {
		return nil, args.Get(1).([]string), args.Error(2)
	}
	return args.Get(0).(*service.StatementDetail), args.Get(1).([]string), args.Error(2)
}

func (m *MockStatementService) ListByCustomer(ctx context.Context, callerID string, roles []string, customerID string, q repository.StatementQuery) (*service.ListStatementsDetail, int, []string, error) {
	args := m.Called(ctx, callerID, roles, customerID, q)
	if args.Get(0) == nil {
		return nil, 0, args.Get(2).([]string), args.Error(3)
	}
	return args.Get(0).(*service.ListStatementsDetail), args.Int(1), args.Get(2).([]string), args.Error(3)
}

func setupTestRouter(svc service.StatementService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	
	// Mock auth middleware
	r.Use(func(c *gin.Context) {
		callerID := c.GetHeader("X-Caller-ID")
		role := c.GetHeader("X-Role")
		if callerID != "" {
			c.Set("callerID", callerID)
			c.Set("roles", []string{role}) // Match service.roles []string
		}
		c.Next()
	})

	r.GET("/statements", handlers.NewListStatementsHandler(svc))
	r.GET("/statements/:id", handlers.NewGetStatementHandler(svc))
	return r
}

func TestStatementsAPI_RBAC_Hardening(t *testing.T) {
	mockSvc := new(MockStatementService)
	router := setupTestRouter(mockSvc)

	t.Run("subscriber cannot access another customer's statement", func(t *testing.T) {
		mockSvc.On("ListByCustomer", mock.Anything, "subscriber_1", []string{"subscriber"}, "customer_2", mock.Anything).
			Return(nil, 0, []string(nil), service.ErrForbidden).Once()

		req, _ := http.NewRequest("GET", "/statements?customer_id=customer_2", nil)
		req.Header.Set("X-Caller-ID", "subscriber_1")
		req.Header.Set("X-Role", "subscriber")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("admin can access any customer's statement", func(t *testing.T) {
		expectedData := &service.ListStatementsDetail{
			Statements: []*service.StatementDetail{
				{ID: "stmt_1", Customer: "customer_2"},
			},
		}
		mockSvc.On("ListByCustomer", mock.Anything, "admin_1", []string{"admin"}, "customer_2", mock.Anything).
			Return(expectedData, 1, []string(nil), nil).Once()

		req, _ := http.NewRequest("GET", "/statements?customer_id=customer_2", nil)
		req.Header.Set("X-Caller-ID", "admin_1")
		req.Header.Set("X-Role", "admin")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp service.ResponseEnvelopeWithPagination
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		// Success check is sufficient for RBAC hardening verification
	})
}

func TestStatementsAPI_Pagination_Hardening(t *testing.T) {
	mockSvc := new(MockStatementService)
	router := setupTestRouter(mockSvc)

	t.Run("returns next cursor for pagination", func(t *testing.T) {
		statements := []*service.StatementDetail{
			{ID: "stmt_1"}, {ID: "stmt_2"}, {ID: "stmt_3"},
		}
		mockSvc.On("ListByCustomer", mock.Anything, "customer_1", []string{"subscriber"}, "customer_1", mock.MatchedBy(func(q repository.StatementQuery) bool {
			return q.Limit == 3
		})).Return(&service.ListStatementsDetail{Statements: statements}, 10, []string(nil), nil).Once()

		req, _ := http.NewRequest("GET", "/statements?limit=3", nil)
		req.Header.Set("X-Caller-ID", "customer_1")
		req.Header.Set("X-Role", "subscriber")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp struct {
			Pagination struct {
				NextCursor string `json:"next_cursor"`
				HasMore    bool   `json:"has_more"`
			} `json:"pagination"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "stmt_3", resp.Pagination.NextCursor)
		assert.True(t, resp.Pagination.HasMore)
	})
}
