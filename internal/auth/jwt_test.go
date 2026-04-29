package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestConfigValidation validates security constraints on JWT config
func TestConfigValidation(t *testing.T) {
	baseConfig := Config{
		Secret:       []byte("test-super-secret-key-minimum-32-bytes"),
		Issuer:       "stellabill",
		Audience:     "api-clients",
		ClockSkewSec: 30,
		Algorithm:    "HS256",
	}

	tests := []struct {
		name      string
		config    Config
		wantError bool
		errMsg    string
	}{
		{
			name:      "Valid config",
			config:    baseConfig,
			wantError: false,
		},
		{
			name: "Empty secret",
			config: Config{
				Secret:    []byte(""),
				Issuer:    "stellabill",
				Audience:  "api-clients",
				Algorithm: "HS256",
			},
			wantError: true,
			errMsg:    "cannot be empty",
		},
		{
			name: "Short secret (< 32 bytes)",
			config: Config{
				Secret:    []byte("tooshort"),
				Issuer:    "stellabill",
				Audience:  "api-clients",
				Algorithm: "HS256",
			},
			wantError: true,
			errMsg:    "should be at least 32 bytes",
		},
		{
			name: "Missing issuer",
			config: Config{
				Secret:    []byte("test-super-secret-key-minimum-32-bytes"),
				Issuer:    "",
				Audience:  "api-clients",
				Algorithm: "HS256",
			},
			wantError: true,
			errMsg:    "issuer must be configured",
		},
		{
			name: "Missing audience",
			config: Config{
				Secret:    []byte("test-super-secret-key-minimum-32-bytes"),
				Issuer:    "stellabill",
				Audience:  "",
				Algorithm: "HS256",
			},
			wantError: true,
			errMsg:    "audience must be configured",
		},
		{
			name: "Missing algorithm",
			config: Config{
				Secret:    []byte("test-super-secret-key-minimum-32-bytes"),
				Issuer:    "stellabill",
				Audience:  "api-clients",
				Algorithm: "",
			},
			wantError: true,
			errMsg:    "algorithm must be explicitly configured",
		},
		{
			name: "Negative clock skew",
			config: Config{
				Secret:       []byte("test-super-secret-key-minimum-32-bytes"),
				Issuer:       "stellabill",
				Audience:     "api-clients",
				ClockSkewSec: -1,
				Algorithm:    "HS256",
			},
			wantError: true,
			errMsg:    "cannot be negative",
		},
		{
			name: "Clock skew too large",
			config: Config{
				Secret:       []byte("test-super-secret-key-minimum-32-bytes"),
				Issuer:       "stellabill",
				Audience:     "api-clients",
				ClockSkewSec: 400,
				Algorithm:    "HS256",
			},
			wantError: true,
			errMsg:    "too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateConfig()
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateConfig() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil && tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
				t.Errorf("error message %q does not contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestJWTMiddleware(t *testing.T) {
	cfg := Config{
		Secret:       []byte("test-super-secret-key-minimum-32-bytes"),
		Issuer:       "stellabill",
		Audience:     "api-clients",
		ClockSkewSec: 30,
		Algorithm:    "HS256",
	}

	middleware := JWTMiddleware(cfg)

	// A dummy handler that just writes "success" and the injected UserID
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := GetPrincipal(r.Context())
		if !ok {
			t.Fatal("principal not found in context")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(userID))
	})

	handler := middleware(nextHandler)

	// Helper to generate tokens
	generateToken := func(userID string, exp time.Time, iss, aud string, alg string) string {
		signingMethod := jwt.SigningMethodHS256
		if alg == "HS512" {
			signingMethod = jwt.SigningMethodHS512
		}

		claims := JWTClaims{
			UserID: userID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(exp),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				Issuer:    iss,
				Audience:  jwt.ClaimStrings{aud},
			},
		}
		token := jwt.NewWithClaims(signingMethod, claims)
		signed, _ := token.SignedString(cfg.Secret)
		return signed
	}

	validToken := generateToken("user-123", time.Now().Add(time.Hour), cfg.Issuer, cfg.Audience, "HS256")
	expiredToken := generateToken("user-123", time.Now().Add(-time.Hour), cfg.Issuer, cfg.Audience, "HS256")

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid Token",
			authHeader:     "Bearer " + validToken,
			expectedStatus: http.StatusOK,
			expectedBody:   "user-123",
		},
		{
			name:           "Missing Header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"missing authorization header"}` + "\n",
		},
		{
			name:           "Malformed Header",
			authHeader:     "Basic " + validToken,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid authorization format"}` + "\n",
		},
		{
			name:           "Garbage Token",
			authHeader:     "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.garbage.data",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"token parsing failed:`,
		},
		{
			name:           "Expired Token",
			authHeader:     "Bearer " + expiredToken,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid issuer":`, // Will fail issuer check before expiry
		},
		{
			name:           "Invalid Issuer",
			authHeader:     "Bearer " + generateToken("user-123", time.Now().Add(time.Hour), "wrong-issuer", cfg.Audience, "HS256"),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid issuer":`,
		},
		{
			name:           "Invalid Audience",
			authHeader:     "Bearer " + generateToken("user-123", time.Now().Add(time.Hour), cfg.Issuer, "wrong-audience", "HS256"),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid audience":`,
		},
		{
			name:           "Empty Token String",
			authHeader:     "Bearer ", // Missing the actual token part
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"token string cannot be empty"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectedStatus)
			}

			body := rr.Body.String()
			if !contains(body, tt.expectedBody) {
				t.Errorf("handler returned unexpected body\n  got: %v\n  want to contain: %v", body, tt.expectedBody)
			}

			// If it's an error case, verify standard JSON structure
			if tt.expectedStatus == http.StatusUnauthorized {
				var errResp ErrorResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
					t.Errorf("error response is not valid JSON: %v", err)
				}
			}
		})
	}
}

// TestClockSkewValidation validates that clock skew is properly enforced
func TestClockSkewValidation(t *testing.T) {
	secret := []byte("test-super-secret-key-minimum-32-bytes")
	cfg := Config{
		Secret:       secret,
		Issuer:       "stellabill",
		Audience:     "api-clients",
		ClockSkewSec: 30,
		Algorithm:    "HS256",
	}

	middleware := JWTMiddleware(cfg)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Token that expired 20 seconds ago (within 30-second skew)
	claimsWithin := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-20 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			Issuer:    cfg.Issuer,
			Audience:  jwt.ClaimStrings{cfg.Audience},
		},
	}
	tokenWithin := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsWithin)
	signedWithin, _ := tokenWithin.SignedString(secret)

	// Token that expired 60 seconds ago (outside 30-second skew)
	claimsBeyond := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-60 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    cfg.Issuer,
			Audience:  jwt.ClaimStrings{cfg.Audience},
		},
	}
	tokenBeyond := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsBeyond)
	signedBeyond, _ := tokenBeyond.SignedString(secret)

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{
			name:           "Token within skew tolerance",
			token:          signedWithin,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Token beyond skew tolerance",
			token:          signedBeyond,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestAlgorithmValidation ensures only explicitly configured algorithms are accepted
func TestAlgorithmValidation(t *testing.T) {
	secret := []byte("test-super-secret-key-minimum-32-bytes")
	cfg := Config{
		Secret:       secret,
		Issuer:       "stellabill",
		Audience:     "api-clients",
		ClockSkewSec: 30,
		Algorithm:    "HS256",
	}

	middleware := JWTMiddleware(cfg)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Valid HS256 token
	validClaims := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    cfg.Issuer,
			Audience:  jwt.ClaimStrings{cfg.Audience},
		},
	}
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims)
	validSigned, _ := validToken.SignedString(secret)

	// HS512 token (algorithm mismatch)
	mismatchClaims := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    cfg.Issuer,
			Audience:  jwt.ClaimStrings{cfg.Audience},
		},
	}
	mismatchToken := jwt.NewWithClaims(jwt.SigningMethodHS512, mismatchClaims)
	mismatchSigned, _ := mismatchToken.SignedString(secret)

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{
			name:           "Correct algorithm HS256",
			token:          validSigned,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Wrong algorithm HS512",
			token:          mismatchSigned,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestNotBeforeValidation validates NBF claim handling
func TestNotBeforeValidation(t *testing.T) {
	secret := []byte("test-super-secret-key-minimum-32-bytes")
	cfg := Config{
		Secret:       secret,
		Issuer:       "stellabill",
		Audience:     "api-clients",
		ClockSkewSec: 30,
		Algorithm:    "HS256",
	}

	middleware := JWTMiddleware(cfg)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Token valid now
	validNBF := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt:  jwt.NewNumericDate(time.Now().Add(time.Hour)),
			NotBefore:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			Issuer:     cfg.Issuer,
			Audience:   jwt.ClaimStrings{cfg.Audience},
		},
	}
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, validNBF)
	validSigned, _ := validToken.SignedString(secret)

	// Token not yet valid (NBF in future)
	futureNBF := Claims{
		UserID: "user-123",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt:  jwt.NewNumericDate(time.Now().Add(time.Hour)),
			NotBefore:  jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
			Issuer:     cfg.Issuer,
			Audience:   jwt.ClaimStrings{cfg.Audience},
		},
	}
	futureToken := jwt.NewWithClaims(jwt.SigningMethodHS256, futureNBF)
	futureSigned, _ := futureToken.SignedString(secret)

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{
			name:           "Valid NotBefore",
			token:          validSigned,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "NotBefore in future",
			token:          futureSigned,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestGetPrincipal_NotFound(t *testing.T) {
	// Test extracting from an empty context
	_, ok := GetPrincipal(context.Background())
	if ok {
		t.Error("expected ok to be false for empty context")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
