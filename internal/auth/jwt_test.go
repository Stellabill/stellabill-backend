package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	testSecret   = "0123456789012345678901234567890ab" // 32 bytes
	testIssuer   = "stellarbill-backend"
	testAudience = "api-clients"
)

func testConfig() Config {
	return Config{
		Secret:       []byte(testSecret),
		Issuer:       testIssuer,
		Audience:     testAudience,
		Algorithm:    "HS256",
		ClockSkewSec: 0,
		MaxTokenAge:  0,
	}
}

func signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testSecret))
	require.NoError(t, err)
	return signed
}

// signWithKey signs claims with an arbitrary key, useful for wrong-key tests.
func signWithKey(t *testing.T, claims jwt.MapClaims, key []byte) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

// signWithAlg signs claims with a specific signing method and key.
func signWithAlg(t *testing.T, method jwt.SigningMethod, claims jwt.MapClaims, key interface{}) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

// executeRequest runs a request through the middleware-under-test and returns
// the recorder and parsed body.
func executeRequest(t *testing.T, cfg Config, req *http.Request) (*httptest.ResponseRecorder, ErrorResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	handler := JWTMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	var body ErrorResponse
	if rr.Body.Len() > 0 {
		_ = json.NewDecoder(rr.Body).Decode(&body)
	}
	return rr, body
}

// validClaims returns a baseline set of claims that pass all checks.
func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":     "user-123",
		"user_id": "user-123",
		"email":   "user@example.com",
		"role":    "admin",
		"iss":     testIssuer,
		"aud":     []string{testAudience},
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}
}

// ---------------------------------------------------------------------------
// Config validation
// ---------------------------------------------------------------------------

func TestValidateConfig_SecretTooShort(t *testing.T) {
	cfg := Config{
		Secret:       []byte("short"),
		Issuer:       testIssuer,
		Audience:     testAudience,
		Algorithm:    "HS256",
		ClockSkewSec: 0,
		MaxTokenAge:  0,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_MissingIssuer(t *testing.T) {
	cfg := Config{
		Secret:       []byte(testSecret),
		Issuer:       "",
		Audience:     testAudience,
		Algorithm:    "HS256",
		ClockSkewSec: 0,
		MaxTokenAge:  0,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_MissingAudience(t *testing.T) {
	cfg := Config{
		Secret:       []byte(testSecret),
		Issuer:       testIssuer,
		Audience:     "",
		Algorithm:    "HS256",
		ClockSkewSec: 0,
		MaxTokenAge:  0,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_MissingAlgorithm(t *testing.T) {
	cfg := Config{
		Secret:       []byte(testSecret),
		Issuer:       testIssuer,
		Audience:     testAudience,
		Algorithm:    "",
		ClockSkewSec: 0,
		MaxTokenAge:  0,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_ClockSkewTooHigh(t *testing.T) {
	cfg := Config{
		Secret:       []byte(testSecret),
		Issuer:       testIssuer,
		Audience:     testAudience,
		Algorithm:    "HS256",
		ClockSkewSec: 301,
		MaxTokenAge:  0,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_NegativeMaxTokenAge(t *testing.T) {
	cfg := Config{
		Secret:       []byte(testSecret),
		Issuer:       testIssuer,
		Audience:     testAudience,
		Algorithm:    "HS256",
		ClockSkewSec: 0,
		MaxTokenAge:  -1,
	}
	assert.Panics(t, func() { cfg.ValidateConfig() })
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := testConfig()
	assert.NotPanics(t, func() { cfg.ValidateConfig() })
}

// ---------------------------------------------------------------------------
// Middleware — happy path
// ---------------------------------------------------------------------------

func TestJWTMiddleware_ValidToken(t *testing.T) {
	cfg := testConfig()
	token := signToken(t, validClaims())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestJWTMiddleware_VerifiesPrincipalInContext(t *testing.T) {
	cfg := testConfig()
	token := signToken(t, validClaims())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()
	var gotPrincipal string
	handler := JWTMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := GetPrincipal(r.Context())
		gotPrincipal = p
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "user-123", gotPrincipal)
}

// ---------------------------------------------------------------------------
// Middleware — missing / malformed header
// ---------------------------------------------------------------------------

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "missing authorization header")
}

func TestJWTMiddleware_InvalidFormat_NoBearerPrefix(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token abc123")

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid authorization format")
}

func TestJWTMiddleware_InvalidFormat_MissingToken(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid authorization format")
}

// ---------------------------------------------------------------------------
// Middleware — expired token
// ---------------------------------------------------------------------------

func TestJWTMiddleware_ExpiredToken(t *testing.T) {
	cfg := testConfig()
	claims := jwt.MapClaims{
		"sub":     "user-123",
		"user_id": "user-123",
		"email":   "user@example.com",
		"role":    "admin",
		"iss":     testIssuer,
		"aud":     []string{testAudience},
		"exp":     time.Now().Add(-1 * time.Hour).Unix(),
		"iat":     time.Now().Add(-2 * time.Hour).Unix(),
	}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token expired")
}

// ---------------------------------------------------------------------------
// Middleware — wrong issuer
// ---------------------------------------------------------------------------

func TestJWTMiddleware_InvalidIssuer(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["iss"] = "malicious-issuer"
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid issuer")
}

// ---------------------------------------------------------------------------
// Middleware — wrong audience
// ---------------------------------------------------------------------------

func TestJWTMiddleware_InvalidAudience(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["aud"] = []string{"wrong-audience"}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid audience")
}

// ---------------------------------------------------------------------------
// Middleware — wrong signature
// ---------------------------------------------------------------------------

func TestJWTMiddleware_WrongSignature(t *testing.T) {
	cfg := testConfig()
	wrongKey := []byte("this-is-a-different-32-byte-key-for-test!")
	token := signWithKey(t, validClaims(), wrongKey)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid or expired token")
}

// ---------------------------------------------------------------------------
// Middleware — algorithm confusion
// ---------------------------------------------------------------------------

func TestJWTMiddleware_WrongAlgorithm(t *testing.T) {
	cfg := testConfig()
	token := signWithAlg(t, jwt.SigningMethodHS512, validClaims(), []byte(testSecret))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid or expired token")
}

func TestJWTMiddleware_NoneAlgorithmRejected(t *testing.T) {
	cfg := testConfig()

	// Craft a token with alg:"none" — the JWT library should reject it
	// because our keyfunc requires HMAC.
	headerBytes, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payloadBytes, _ := json.Marshal(validClaims())
	encoded := base64.RawURLEncoding.EncodeToString(headerBytes) +
		"." + base64.RawURLEncoding.EncodeToString(payloadBytes) + "."

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+encoded)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid or expired token")
}

// ---------------------------------------------------------------------------
// Middleware — not-before (nbf)
// ---------------------------------------------------------------------------

func TestJWTMiddleware_NotBeforeInFuture(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["nbf"] = time.Now().Add(1 * time.Hour).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token not valid until")
}

func TestJWTMiddleware_NotBeforeInPast(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["nbf"] = time.Now().Add(-1 * time.Hour).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---------------------------------------------------------------------------
// Clock skew
// ---------------------------------------------------------------------------

func TestClockSkew_AcceptsRecentlyExpiredToken(t *testing.T) {
	cfg := testConfig()
	cfg.ClockSkewSec = 60
	// Token expired 30 seconds ago — within skew
	claims := validClaims()
	claims["exp"] = time.Now().Add(-30 * time.Second).Unix()
	claims["iat"] = time.Now().Add(-1*time.Hour - 30*time.Second).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestClockSkew_RejectsTokenExpiredBeyondSkew(t *testing.T) {
	cfg := testConfig()
	cfg.ClockSkewSec = 60
	// Token expired 120 seconds ago — beyond 60s skew
	claims := validClaims()
	claims["exp"] = time.Now().Add(-120 * time.Second).Unix()
	claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token expired")
}

// ---------------------------------------------------------------------------
// MaxTokenAge
// ---------------------------------------------------------------------------

func TestMaxTokenAge_RejectsOldToken(t *testing.T) {
	cfg := testConfig()
	cfg.MaxTokenAge = 3600 // 1 hour
	claims := validClaims()
	// Issued 2 hours ago, expires in 10 minutes (still valid by exp check)
	claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
	claims["exp"] = time.Now().Add(10 * time.Minute).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token too old")
}

func TestMaxTokenAge_AcceptsFreshToken(t *testing.T) {
	cfg := testConfig()
	cfg.MaxTokenAge = 3600
	claims := validClaims()
	claims["iat"] = time.Now().Add(-30 * time.Minute).Unix()
	claims["exp"] = time.Now().Add(30 * time.Minute).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---------------------------------------------------------------------------
// Missing required claims
// ---------------------------------------------------------------------------

func TestJWTMiddleware_MissingExpiry(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	delete(claims, "exp")
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token expiration claim missing")
}

// ---------------------------------------------------------------------------
// TokenGenerator
// ---------------------------------------------------------------------------

func TestTokenGenerator_GenerateAdminToken(t *testing.T) {
	gen := NewTokenGenerator(testSecret)
	tokenStr, err := gen.GenerateAdminToken("u1", "admin@test.com")
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims := token.Claims.(*Claims)
	assert.Equal(t, "u1", claims.UserID)
	assert.Equal(t, RoleAdmin, claims.Role)
	assert.Equal(t, "stellarbill-backend", claims.Issuer)
	assert.Equal(t, jwt.ClaimStrings{"api-clients"}, claims.Audience)
}

func TestTokenGenerator_GenerateExpiredToken(t *testing.T) {
	gen := NewTokenGenerator(testSecret)
	tokenStr, err := gen.GenerateExpiredToken("u1", "admin@test.com", RoleAdmin)
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	}, jwt.WithLeeway(time.Hour*24*365))
	require.NoError(t, err)
	claims := token.Claims.(*Claims)
	assert.True(t, claims.ExpiresAt.Time.Before(time.Now()))
}

func TestTokenGenerator_GenerateMerchantToken(t *testing.T) {
	gen := NewTokenGenerator(testSecret)
	tokenStr, err := gen.GenerateMerchantToken("m1", "merchant@test.com", "merchant-1")
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims := token.Claims.(*Claims)
	assert.Equal(t, "m1", claims.UserID)
	assert.Equal(t, RoleMerchant, claims.Role)
}

func TestTokenGenerator_GenerateCustomerToken(t *testing.T) {
	gen := NewTokenGenerator(testSecret)
	tokenStr, err := gen.GenerateCustomerToken("c1", "customer@test.com")
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(testSecret), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims := token.Claims.(*Claims)
	assert.Equal(t, "c1", claims.UserID)
	assert.Equal(t, RoleCustomer, claims.Role)
}

// ---------------------------------------------------------------------------
// GetPrincipal
// ---------------------------------------------------------------------------

func TestGetPrincipal_NotFound(t *testing.T) {
	val, ok := GetPrincipal(nil)
	assert.False(t, ok)
	assert.Empty(t, val)
}

// ---------------------------------------------------------------------------
// Concurrent access safety
// ---------------------------------------------------------------------------

func TestJWTMiddleware_ConcurrentRequests(t *testing.T) {
	cfg := testConfig()
	const goroutines = 50
	done := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			token := signToken(t, validClaims())
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)

			rr, _ := executeRequest(t, cfg, req)
			if rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rr.Code)
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility
// ---------------------------------------------------------------------------

func TestJWTMiddleware_BackwardCompat_ValidIssuerAudience(t *testing.T) {
	// Tokens from the expected issuer with the correct audience must pass
	// through — this is the primary backward-compat guarantee.
	cfg := testConfig()

	claims := validClaims()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestJWTMiddleware_BackwardCompat_MultipleAudiences(t *testing.T) {
	// Tokens carrying multiple audiences should pass when our required
	// audience is among them.
	cfg := testConfig()

	claims := validClaims()
	claims["aud"] = []string{"other-client", testAudience, "yet-another"}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestJWTMiddleware_BackwardCompat_MinimalClaims(t *testing.T) {
	// A token with only the mandatory claims (sub, user_id, iss, aud, exp)
	// should still be accepted.
	cfg := testConfig()

	claims := jwt.MapClaims{
		"sub":     "user-456",
		"user_id": "user-456",
		"email":   "minimal@example.com",
		"role":    "customer",
		"iss":     testIssuer,
		"aud":     []string{testAudience},
		"exp":     time.Now().Add(1 * time.Hour).Unix(),
	}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---------------------------------------------------------------------------
// Regression: empty / malformed token strings
// ---------------------------------------------------------------------------

func TestJWTMiddleware_EmptyToken(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid authorization format")
}

func TestJWTMiddleware_GarbageToken(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid or expired token")
}

func TestJWTMiddleware_TooManyHeaderParts(t *testing.T) {
	cfg := testConfig()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token extraneous")

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid authorization format")
}

// ---------------------------------------------------------------------------
// Regression: boundary clock skew tests
// ---------------------------------------------------------------------------

func TestClockSkew_ExactlyAtBoundaryAccepts(t *testing.T) {
	cfg := testConfig()
	cfg.ClockSkewSec = 60
	claims := validClaims()
	// Token expired just under 60 seconds ago — within skew.
	claims["exp"] = time.Now().Add(-59 * time.Second).Unix()
	claims["iat"] = time.Now().Add(-1*time.Hour - 59*time.Second).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestClockSkew_JustBeyondBoundaryRejects(t *testing.T) {
	cfg := testConfig()
	cfg.ClockSkewSec = 60
	claims := validClaims()
	// Token expired 120 seconds ago — well beyond 60s skew.
	claims["exp"] = time.Now().Add(-120 * time.Second).Unix()
	claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "token expired")
}

// ---------------------------------------------------------------------------
// Regression: empty audience list in token
// ---------------------------------------------------------------------------

func TestJWTMiddleware_EmptyAudienceList(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["aud"] = []string{}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, body := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, body.Error, "invalid audience")
}

// ---------------------------------------------------------------------------
// Regression: token with roles array
// ---------------------------------------------------------------------------

func TestJWTMiddleware_TokenWithRolesArray(t *testing.T) {
	cfg := testConfig()
	claims := validClaims()
	claims["roles"] = []string{"admin", "merchant"}
	token := signToken(t, claims)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr, _ := executeRequest(t, cfg, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}
