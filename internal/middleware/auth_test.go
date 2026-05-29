package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/auth"
)

const testSecret = "test-secret-value"

func makeToken(t *testing.T, claims jwt.MapClaims, key interface{}, method jwt.SigningMethod) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": "user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func newTestRouter(secret string) (*gin.Engine, *bool) {
	gin.SetMode(gin.TestMode)
	reached := false
	r := gin.New()
	r.GET("/protected", AuthMiddleware(nil, secret), func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})
	return r, &reached
}

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantPass   bool
	}{
		{
			name:       "missing Authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name:       "wrong scheme (Basic)",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name:       "Bearer with empty token",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name:       "Bearer with whitespace-only token",
			authHeader: "Bearer    ",
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name:       "malformed token (random string)",
			authHeader: "Bearer not.a.jwt",
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name: "expired token",
			authHeader: "Bearer " + func() string {
				claims := jwt.MapClaims{
					"sub": "user-123",
					"exp": time.Now().Add(-time.Hour).Unix(),
				}
				tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				s, _ := tok.SignedString([]byte(testSecret))
				return s
			}(),
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name: "wrong signing key",
			authHeader: "Bearer " + func() string {
				claims := validClaims()
				tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				s, _ := tok.SignedString([]byte("wrong-secret"))
				return s
			}(),
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name: "alg:none attack",
			authHeader: "Bearer " + func() string {
				claims := validClaims()
				tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
				s, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
				return s
			}(),
			wantStatus: http.StatusUnauthorized,
			wantPass:   false,
		},
		{
			name: "valid token passes through",
			authHeader: "Bearer " + func() string {
				claims := validClaims()
				tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				s, _ := tok.SignedString([]byte(testSecret))
				return s
			}(),
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, reached := newTestRouter(testSecret)

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantPass, *reached)
		})
	}
}

func TestAuthMiddleware_ContextValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var (
		gotSub   string
		gotRoles []auth.Role
	)

	claims := jwt.MapClaims{
		"sub":   "user-456",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"roles": []interface{}{"admin", "editor"},
	}
	token := makeToken(t, claims, []byte(testSecret), jwt.SigningMethodHS256)

	r := gin.New()
	r.GET("/protected", AuthMiddleware(nil, testSecret), func(c *gin.Context) {
		// Fix: type-assert the any returned by c.Get
		if v, exists := c.Get(ContextKeySubject); exists {
			gotSub, _ = v.(string)
		}
		if v, exists := c.Get(auth.RolesContextKey); exists {
			gotRoles, _ = v.([]auth.Role)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "user-456", gotSub)
	assert.Equal(t, []auth.Role{"admin", "editor"}, gotRoles)
}

func TestExtractRoles(t *testing.T) {
	tests := []struct {
		name  string
		input jwt.MapClaims
		want  []auth.Role
	}{
		{
			name:  "no roles claim",
			input: jwt.MapClaims{},
			want:  nil,
		},
		{
			name:  "roles as string array",
			input: jwt.MapClaims{"roles": []interface{}{"admin", "viewer"}},
			want:  []auth.Role{"admin", "viewer"},
		},
		{
			name:  "roles as single string",
			input: jwt.MapClaims{"roles": "admin"},
			want:  []auth.Role{"admin"},
		},
		{
			name:  "roles as empty string",
			input: jwt.MapClaims{"roles": ""},
			want:  nil,
		},
		{
			name:  "roles array with blank entries filtered out",
			input: jwt.MapClaims{"roles": []interface{}{"admin", "  ", "editor"}},
			want:  []auth.Role{"admin", "editor"},
		},
		{
			name:  "roles as unexpected type",
			input: jwt.MapClaims{"roles": 42},
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractRoles(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMapJWTError(t *testing.T) {
	tests := []struct {
		name    string
		input   error
		wantMsg string
	}{
		{
			name:    "expired",
			input:   jwt.ErrTokenExpired,
			wantMsg: "token has expired",
		},
		{
			name:    "not yet valid",
			input:   jwt.ErrTokenNotValidYet,
			wantMsg: "token is not yet valid",
		},
		{
			name:    "invalid signature",
			input:   jwt.ErrTokenSignatureInvalid,
			wantMsg: "token signature is invalid",
		},
		{
			name:    "malformed",
			input:   jwt.ErrTokenMalformed,
			wantMsg: "token is malformed",
		},
		{
			name:    "unknown error falls back to generic",
			input:   jwt.ErrTokenInvalidClaims,
			wantMsg: "token is invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapJWTError(tc.input)
			require.Error(t, err)
			assert.Equal(t, tc.wantMsg, err.Error())
		})
	}
}
