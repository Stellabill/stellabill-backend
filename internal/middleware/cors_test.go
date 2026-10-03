package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name               string
		env                string
		allowedOrigins     string
		reqOrigin          string
		reqMethod          string
		reqHeaders         string
		expectedStatus     int
		expectedAllowOrig  string
		expectedAllowCreds string
		expectedAllowHdrs  string
	}{
		{
			name:               "Production - Allowed origin",
			env:                "production",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://example.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "https://example.com",
			expectedAllowCreds: "true",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "Production - Case insensitive allowed origin",
			env:                "production",
			allowedOrigins:     "https://EXAMPLE.com",
			reqOrigin:          "https://example.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "https://example.com",
			expectedAllowCreds: "true",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "Production - Disallowed origin",
			env:                "production",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://hacker.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK, // Request proceeds but no CORS headers
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
		{
			name:               "Production - Preflight allowed origin",
			env:                "production",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://example.com",
			reqMethod:          "OPTIONS",
			reqHeaders:         "X-Request-ID, X-Stellabill-Date, X-Stellabill-Signature",
			expectedStatus:     http.StatusNoContent,
			expectedAllowOrig:  "https://example.com",
			expectedAllowCreds: "true",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Stellabill-Date, X-Stellabill-Signature",
		},
		{
			name:               "Production - Preflight disallowed origin",
			env:                "production",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://hacker.com",
			reqMethod:          "OPTIONS",
			expectedStatus:     http.StatusForbidden,
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
		{
			name:               "Production - Wildcard is ignored",
			env:                "production",
			allowedOrigins:     "*",
			reqOrigin:          "https://example.com",
			reqMethod:          "OPTIONS",
			expectedStatus:     http.StatusForbidden,
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
		{
			name:               "Production - Empty allowed origins",
			env:                "production",
			allowedOrigins:     "",
			reqOrigin:          "https://example.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
		{
			name:               "Staging - Allowed origin",
			env:                "staging",
			allowedOrigins:     "https://staging.example.com",
			reqOrigin:          "https://staging.example.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "https://staging.example.com",
			expectedAllowCreds: "true",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "Development - Allowed origin",
			env:                "development",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://example.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "https://example.com",
			expectedAllowCreds: "true",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "Development - Disallowed origin",
			env:                "development",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "https://hacker.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
		{
			name:               "Development - Wildcard allow all",
			env:                "development",
			allowedOrigins:     "*",
			reqOrigin:          "https://hacker.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "*",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "Development - Empty origins allow all",
			env:                "development",
			allowedOrigins:     "",
			reqOrigin:          "https://hacker.com",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "*",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "Content-Type, Authorization, Idempotency-Key, X-Request-ID, X-Admin-Token, X-Stellabill-Date, X-Stellabill-Request-ID, X-Stellabill-Signature",
		},
		{
			name:               "No Origin header",
			env:                "production",
			allowedOrigins:     "https://example.com",
			reqOrigin:          "",
			reqMethod:          "GET",
			expectedStatus:     http.StatusOK,
			expectedAllowOrig:  "",
			expectedAllowCreds: "",
			expectedAllowHdrs:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(CORS(tt.env, tt.allowedOrigins))
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req, _ := http.NewRequest(tt.reqMethod, "/test", nil)
			if tt.reqOrigin != "" {
				req.Header.Set("Origin", tt.reqOrigin)
			}
			if tt.reqHeaders != "" {
				req.Header.Set("Access-Control-Request-Headers", tt.reqHeaders)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			assert.Equal(t, tt.expectedAllowOrig, w.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, tt.expectedAllowCreds, w.Header().Get("Access-Control-Allow-Credentials"))
			if tt.expectedAllowHdrs != "" {
				assert.Equal(t, tt.expectedAllowHdrs, w.Header().Get("Access-Control-Allow-Headers"))
			}
		})
	}
}
