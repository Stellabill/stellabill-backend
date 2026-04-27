package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTMiddleware(t *testing.T) {
	cfg := Config{
		Secret:   []byte("test-super-secret"),
		Issuer:   "stellabill",
		Audience: "api-clients",
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
	generateToken := func(userID string, exp time.Time, iss, aud string) string {
		claims := Claims{
			UserID: userID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(exp),
				Issuer:    iss,
				Audience:  jwt.ClaimStrings{aud},
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, _ := token.SignedString(cfg.Secret)
		return signed
	}

	validToken := generateToken("user-123", time.Now().Add(time.Hour), cfg.Issuer, cfg.Audience)
	expiredToken := generateToken("user-123", time.Now().Add(-time.Hour), cfg.Issuer, cfg.Audience)

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
			expectedBody:   `{"error":"invalid or expired token"}` + "\n",
		},
		{
			name:           "Expired Token",
			authHeader:     "Bearer " + expiredToken,
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid or expired token"}` + "\n",
		},
		{
			name:           "Invalid Issuer",
			authHeader:     "Bearer " + generateToken("user-123", time.Now().Add(time.Hour), "wrong-issuer", cfg.Audience),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid issuer"}` + "\n",
		},
		{
			name:           "Invalid Audience",
			authHeader:     "Bearer " + generateToken("user-123", time.Now().Add(time.Hour), cfg.Issuer, "wrong-audience"),
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid audience"}` + "\n",
		},
		{
			name:           "Empty Token String",
			authHeader:     "Bearer ", // Missing the actual token part
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"invalid authorization format"}` + "\n",
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

			if rr.Body.String() != tt.expectedBody {
				t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), tt.expectedBody)
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

func TestGetPrincipal_NotFound(t *testing.T) {
	// Test extracting from an empty context
	_, ok := GetPrincipal(context.Background())
	if ok {
		t.Error("expected ok to be false for empty context")
	}
}

