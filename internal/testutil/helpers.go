package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/auth"
)

// TestRequest holds test request context
type TestRequest struct {
	Router *gin.Engine
	Token  string
}

// NewTestRequest creates a new test request builder
func NewTestRequest(router *gin.Engine) *TestRequest {
	return &TestRequest{Router: router}
}

// WithToken sets the Authorization header with a Bearer token
func (tr *TestRequest) WithToken(token string) *TestRequest {
	tr.Token = token
	return tr
}

// Get performs a GET request
func (tr *TestRequest) Get(path string) *TestResponse {
	req, _ := http.NewRequest("GET", path, nil)
	return tr.sendRequest(req)
}

// Post performs a POST request with JSON body
func (tr *TestRequest) Post(path string, body interface{}) *TestResponse {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest("POST", path, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	return tr.sendRequest(req)
}

// Put performs a PUT request with JSON body
func (tr *TestRequest) Put(path string, body interface{}) *TestResponse {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest("PUT", path, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	return tr.sendRequest(req)
}

// Delete performs a DELETE request
func (tr *TestRequest) Delete(path string) *TestResponse {
	req, _ := http.NewRequest("DELETE", path, nil)
	return tr.sendRequest(req)
}

// sendRequest executes the request and returns response
func (tr *TestRequest) sendRequest(req *http.Request) *TestResponse {
	if tr.Token != "" {
		req.Header.Set("Authorization", "Bearer "+tr.Token)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	tr.Router.ServeHTTP(w, req)

	return &TestResponse{
		Response: w.Result(),
		Body:     w.Body.String(),
	}
}

// TestResponse holds response data
type TestResponse struct {
	Response *http.Response
	Body     string
}

// Status returns the HTTP status code
func (tr *TestResponse) Status() int {
	return tr.Response.StatusCode
}

// JSON unmarshals the response body as JSON
func (tr *TestResponse) JSON(v interface{}) error {
	return json.Unmarshal([]byte(tr.Body), v)
}

// BodyString returns the raw body as string
func (tr *TestResponse) BodyString() string {
	return tr.Body
}

// HasError checks if response contains error field
func (tr *TestResponse) HasError() bool {
	var respMap map[string]interface{}
	if err := json.Unmarshal([]byte(tr.Body), &respMap); err != nil {
		return false
	}
	_, exists := respMap["error"]
	return exists
}

// GetError extracts error message from response
func (tr *TestResponse) GetError() string {
	var respMap map[string]interface{}
	if err := json.Unmarshal([]byte(tr.Body), &respMap); err != nil {
		return ""
	}
	if errMsg, exists := respMap["error"]; exists {
		if errStr, ok := errMsg.(string); ok {
			return errStr
		}
	}
	return ""
}

// TestTokenGenerator is a convenience wrapper around auth.TokenGenerator for tests
type TestTokenGenerator struct {
	*auth.TokenGenerator
}

// NewTestTokenGenerator creates a test token generator
func NewTestTokenGenerator(jwtSecret string) *TestTokenGenerator {
	return &TestTokenGenerator{
		TokenGenerator: auth.NewTokenGenerator(jwtSecret),
	}
}

// TestAuthScenarios defines test scenarios for auth/authz
type TestAuthScenario struct {
	Name          string
	Token         string
	ExpectedCode  int
	ExpectedError string
}

// CommonAuthScenarios returns common authentication test scenarios
func CommonAuthScenarios(tg *TestTokenGenerator) []TestAuthScenario {
	adminToken, _ := tg.GenerateAdminToken("admin-user", "admin@test.com")
	merchantToken, _ := tg.GenerateMerchantToken("merchant-user", "merchant@test.com", "merchant-123")
	customerToken, _ := tg.GenerateCustomerToken("customer-user", "customer@test.com")
	expiredToken, _ := tg.GenerateExpiredToken("user", "user@test.com", auth.RoleAdmin)

	return []TestAuthScenario{
		{
			Name:          "no token",
			Token:         "",
			ExpectedCode:  http.StatusUnauthorized,
			ExpectedError: "missing authorization header",
		},
		{
			Name:          "malformed header",
			Token:         "InvalidHeader",
			ExpectedCode:  http.StatusUnauthorized,
			ExpectedError: "invalid authorization header format",
		},
		{
			Name:          "expired token",
			Token:         expiredToken,
			ExpectedCode:  http.StatusUnauthorized,
			ExpectedError: "invalid or expired token",
		},
		{
			Name:          "valid admin token",
			Token:         adminToken,
			ExpectedCode:  http.StatusOK,
			ExpectedError: "",
		},
		{
			Name:          "valid merchant token",
			Token:         merchantToken,
			ExpectedCode:  http.StatusOK,
			ExpectedError: "",
		},
		{
			Name:          "valid customer token",
			Token:         customerToken,
			ExpectedCode:  http.StatusOK,
			ExpectedError: "",
		},
	}
}

// AdminOnlyScenarios returns scenarios for admin-only endpoints
func AdminOnlyScenarios(tg *TestTokenGenerator) []TestAuthScenario {
	_, _ = tg.GenerateAdminToken("admin-user", "admin@test.com") // verify admin token generation works
	merchantToken, _ := tg.GenerateMerchantToken("merchant-user", "merchant@test.com", "merchant-123")

	scenarios := CommonAuthScenarios(tg)
	scenarios = append(scenarios, TestAuthScenario{
		Name:          "merchant token denied",
		Token:         merchantToken,
		ExpectedCode:  http.StatusForbidden,
		ExpectedError: "insufficient permissions",
	})

	// Update the valid admin token scenario
	for i, s := range scenarios {
		if s.Name == "valid admin token" {
			scenarios[i].ExpectedCode = http.StatusOK
		}
	}

	return scenarios
}

