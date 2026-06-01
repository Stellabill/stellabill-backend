package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminSigningMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test_admin_secret_key_123456789"
	cfg := &AdminSigningConfig{
		SecretKey: secret,
	}
	middleware := AdminSigningMiddleware(cfg)

	tests := []struct {
		name           string
		setupRequest   func() (*httptest.ResponseRecorder, *http.Request)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "valid_signature",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				date := fmt.Sprintf("%d", time.Now().Unix())
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "attempt=1&target=billing-cache", date, requestID, body)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge?attempt=1&target=billing-cache", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, date)
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "invalid_signature",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				date := fmt.Sprintf("%d", time.Now().Unix())

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, date)
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"=invalid_signature")
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminInvalidSignature.Error(),
		},
		{
			name: "missing_signature",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				date := fmt.Sprintf("%d", time.Now().Unix())

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, date)
				req.Header.Set(AdminRequestIDHeader, requestID)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminMissingSignature.Error(),
		},
		{
			name: "missing_date",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", "0", requestID, body)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminMissingDate.Error(),
		},
		{
			name: "missing_request_id",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				date := fmt.Sprintf("%d", time.Now().Unix())
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", date, "", body)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, date)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminMissingRequestID.Error(),
		},
		{
			name: "timestamp_too_old",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				oldDate := fmt.Sprintf("%d", time.Now().Add(-2*time.Minute).Unix())
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", oldDate, requestID, body)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, oldDate)
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminTimestampSkew.Error(),
		},
		{
			name: "timestamp_too_new",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				futureDate := fmt.Sprintf("%d", time.Now().Add(2*time.Minute).Unix())
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", futureDate, requestID, body)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req.Header.Set(AdminDateHeader, futureDate)
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminTimestampSkew.Error(),
		},
		{
			name: "replay_attack_detected",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				body := []byte(`{"partial": "0"}`)
				requestID := uuid.New().String()
				date := fmt.Sprintf("%d", time.Now().Unix())
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", date, requestID, body)
				sig := generateAdminSignature(canonicalReq, secret)

				// First request
				r1 := httptest.NewRecorder()
				req1 := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req1.Header.Set(AdminDateHeader, date)
				req1.Header.Set(AdminRequestIDHeader, requestID)
				req1.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)

				router1 := gin.New()
				router1.POST("/api/admin/purge", middleware, func(c *gin.Context) {
					c.Status(http.StatusOK)
				})
				router1.ServeHTTP(r1, req1)
				assert.Equal(t, http.StatusOK, r1.Code)

				// Second request
				r2 := httptest.NewRecorder()
				req2 := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(body)))
				req2.Header.Set(AdminDateHeader, date)
				req2.Header.Set(AdminRequestIDHeader, requestID)
				req2.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r2, req2
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminReplayDetected.Error(),
		},
		{
			name: "body_mutation_invalidates_signature",
			setupRequest: func() (*httptest.ResponseRecorder, *http.Request) {
				originalBody := []byte(`{"partial": "0"}`)
				mutatedBody := []byte(`{"partial": "1"}`)
				requestID := uuid.New().String()
				date := fmt.Sprintf("%d", time.Now().Unix())
				// Generate signature for original body
				canonicalReq := buildTestCanonicalRequest("POST", "/api/admin/purge", "", date, requestID, originalBody)
				sig := generateAdminSignature(canonicalReq, secret)

				r := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/admin/purge", strings.NewReader(string(mutatedBody)))
				req.Header.Set(AdminDateHeader, date)
				req.Header.Set(AdminRequestIDHeader, requestID)
				req.Header.Set(AdminSignatureHeader, AdminSignatureVersion+"="+sig)
				return r, req
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  ErrAdminInvalidSignature.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, req := tt.setupRequest()
			if req == nil {
				return
			}

			router := gin.New()
			router.POST("/api/admin/purge", middleware, func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			router.ServeHTTP(r, req)

			assert.Equal(t, tt.expectedStatus, r.Code)
			if tt.expectedError != "" {
				assert.Contains(t, r.Body.String(), tt.expectedError)
			}
		})
	}
}

func buildTestCanonicalRequest(method, path, queryStr, date, requestID string, body []byte) string {
	signedHeaders := []string{strings.ToLower(AdminDateHeader), strings.ToLower(AdminRequestIDHeader)}
	headerValues := map[string]string{
		strings.ToLower(AdminDateHeader):      date,
		strings.ToLower(AdminRequestIDHeader): requestID,
	}

	var headersPart strings.Builder
	for _, key := range signedHeaders {
		headersPart.WriteString(fmt.Sprintf("%s:%s\n", key, strings.TrimSpace(headerValues[key])))
	}
	signedHeadersStr := strings.Join(signedHeaders, ";")

	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	var canonicalReq strings.Builder
	canonicalReq.WriteString(method + "\n")
	canonicalReq.WriteString(path + "\n")
	canonicalReq.WriteString(queryStr + "\n")
	canonicalReq.WriteString(headersPart.String() + "\n")
	canonicalReq.WriteString(signedHeadersStr + "\n")
	canonicalReq.WriteString(bodyHashHex)

	return canonicalReq.String()
}

func generateAdminSignature(canonicalReq, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(canonicalReq))
	return hex.EncodeToString(h.Sum(nil))
}
