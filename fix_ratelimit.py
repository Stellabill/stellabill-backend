import re

with open("internal/routes/ratelimit_integration_test.go", "r") as f:
    content = f.read()

# Insert the token generator
token_setup = """
func getAuthToken() string {
	token, _ := createToken("Test1!JwtSecret-MixedAlphaNumeric@123", "user123", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
	return "Bearer " + token
}
"""

content = content.replace("func resetRateLimitEnv() {", token_setup + "\nfunc resetRateLimitEnv() {")

# Replace httptest.NewRequest(...) with a wrapper that adds the token
wrapper = """
func newAuthRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", getAuthToken())
	return req
}
"""

content = content.replace("func setupRouter() *gin.Engine {", wrapper + "\nfunc setupRouter() *gin.Engine {")

# Now replace all httptest.NewRequest in ratelimit tests with newAuthRequest
# EXCEPT for /api/health which can stay httptest.NewRequest (or we can just replace all)
content = re.sub(r'httptest\.NewRequest\("GET", path, nil\)', r'newAuthRequest("GET", path)', content)
content = re.sub(r'httptest\.NewRequest\("GET", "/api/v1/subscriptions", nil\)', r'newAuthRequest("GET", "/api/v1/subscriptions")', content)

with open("internal/routes/ratelimit_integration_test.go", "w") as f:
    f.write(content)
