package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	AdminSignatureHeader = "X-Stellabill-Signature"
	AdminDateHeader      = "X-Stellabill-Date"
	AdminRequestIDHeader = "X-Stellabill-Request-ID"
	AdminSignatureVersion = "v1"
	AdminTimestampSkew   = 60
)

var (
	ErrAdminInvalidSignature = errors.New("invalid admin request signature")
	ErrAdminMissingSignature = errors.New("missing admin request signature")
	ErrAdminMissingDate      = errors.New("missing X-Stellabill-Date header")
	ErrAdminMissingRequestID = errors.New("missing X-Stellabill-Request-ID header")
	ErrAdminTimestampSkew    = errors.New("timestamp skew exceeds 60 seconds")
	ErrAdminReplayDetected   = errors.New("request replay detected")
)

type AdminSigningConfig struct {
	SecretKey string
}

func AdminSigningMiddleware(cfg *AdminSigningConfig) gin.HandlerFunc {
	replayCache := NewEventIDCache(5 * time.Minute)

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		date := c.GetHeader(AdminDateHeader)
		if date == "" {
			abortAdminRequest(c, http.StatusUnauthorized, ErrAdminMissingDate)
			return
		}

		requestID := c.GetHeader(AdminRequestIDHeader)
		if requestID == "" {
			abortAdminRequest(c, http.StatusUnauthorized, ErrAdminMissingRequestID)
			return
		}

		signature := c.GetHeader(AdminSignatureHeader)
		if signature == "" {
			abortAdminRequest(c, http.StatusUnauthorized, ErrAdminMissingSignature)
			return
		}

		if err := verifyAdminTimestamp(date); err != nil {
			abortAdminRequest(c, http.StatusUnauthorized, err)
			return
		}

		if err := replayCache.CheckAndStore(ctx, requestID); err != nil {
			if errors.Is(err, ErrEventIDAlreadySeen) {
				abortAdminRequest(c, http.StatusUnauthorized, ErrAdminReplayDetected)
			} else {
				abortAdminRequest(c, http.StatusBadRequest, err)
			}
			return
		}

		rawBody, err := c.GetRawData()
		if err != nil {
			abortAdminRequest(c, http.StatusBadRequest, fmt.Errorf("failed to read request body: %w", err))
			return
		}

		c.Request.Body = &readSeekCloser{data: rawBody, pos: 0}

		canonicalRequest := buildCanonicalRequest(c.Request, rawBody)

		if err := verifyAdminSignature(canonicalRequest, signature, cfg.SecretKey); err != nil {
			abortAdminRequest(c, http.StatusUnauthorized, err)
			return
		}

		c.Next()
	}
}

func buildCanonicalRequest(req *http.Request, body []byte) string {
	method := req.Method

	path := req.URL.Path
	if path == "" {
		path = "/"
	}

	query := req.URL.Query()
	var sortedQuery []string
	for key, values := range query {
		for _, value := range values {
			sortedQuery = append(sortedQuery, fmt.Sprintf("%s=%s", url.QueryEscape(key), url.QueryEscape(value)))
		}
	}
	sort.Strings(sortedQuery)
	queryString := strings.Join(sortedQuery, "&")

	var signedHeaders []string
	headerValues := make(map[string]string)

	dateHeader := req.Header.Get(AdminDateHeader)
	signedHeaders = append(signedHeaders, strings.ToLower(AdminDateHeader))
	headerValues[strings.ToLower(AdminDateHeader)] = dateHeader

	requestIDHeader := req.Header.Get(AdminRequestIDHeader)
	signedHeaders = append(signedHeaders, strings.ToLower(AdminRequestIDHeader))
	headerValues[strings.ToLower(AdminRequestIDHeader)] = requestIDHeader

	sort.Strings(signedHeaders)

	var headersPart bytes.Buffer
	for _, key := range signedHeaders {
		headersPart.WriteString(fmt.Sprintf("%s:%s\n", key, strings.TrimSpace(headerValues[key])))
	}

	signedHeadersStr := strings.Join(signedHeaders, ";")

	bodyHash := sha256.Sum256(body)
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	var canonicalRequest bytes.Buffer
	canonicalRequest.WriteString(method + "\n")
	canonicalRequest.WriteString(path + "\n")
	canonicalRequest.WriteString(queryString + "\n")
	canonicalRequest.WriteString(headersPart.String() + "\n")
	canonicalRequest.WriteString(signedHeadersStr + "\n")
	canonicalRequest.WriteString(bodyHashHex)

	return canonicalRequest.String()
}

func verifyAdminTimestamp(date string) error {
	secs, err := parseUnixTimestamp(date)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAdminInvalidSignature, err)
	}

	now := time.Now().Unix()
	minTime := now - AdminTimestampSkew
	maxTime := now + AdminTimestampSkew

	if secs < minTime || secs > maxTime {
		return ErrAdminTimestampSkew
	}

	return nil
}

func verifyAdminSignature(canonicalRequest string, signature string, secretKey string) error {
	if strings.HasPrefix(signature, AdminSignatureVersion+"=") {
		signature = strings.TrimPrefix(signature, AdminSignatureVersion+"=")
	}

	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: invalid hex encoding: %v", ErrAdminInvalidSignature, err)
	}

	computedHash := hmac.New(sha256.New, []byte(secretKey))
	computedHash.Write([]byte(canonicalRequest))
	computedSig := computedHash.Sum(nil)

	if !hmac.Equal(sigBytes, computedSig) {
		return ErrAdminInvalidSignature
	}

	return nil
}

func abortAdminRequest(c *gin.Context, statusCode int, err error) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.AbortWithStatusJSON(statusCode, gin.H{
		"error": err.Error(),
	})
}
