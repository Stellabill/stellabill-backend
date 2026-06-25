// Package s3 provides a minimal S3 uploader and presign helper.
// It uses only the standard library (crypto/hmac, crypto/sha256, net/http)
// so no AWS SDK dependency is required. Callers interact only with the
// S3Uploader interface, making tests straightforward with a mock.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// PresignedURL is the result of a successful presign operation.
type PresignedURL struct {
	URL       string
	ExpiresAt time.Time
}

// S3Uploader is the interface callers depend on. Both the real AWS
// implementation and test mocks satisfy this interface.
type S3Uploader interface {
	// PutObject uploads body to the given key in the configured bucket.
	// It retries transient (5xx) errors with exponential backoff.
	PutObject(ctx context.Context, key string, body []byte, contentType string) error

	// PresignURL generates a presigned GET URL for the given key.
	// ttl controls how long the URL is valid.
	PresignURL(ctx context.Context, key string, ttl time.Duration) (PresignedURL, error)
}

// Config holds the credentials and bucket configuration.
type Config struct {
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// Endpoint overrides the default AWS endpoint (useful for localstack).
	Endpoint string
	// MaxRetries is the number of retry attempts for 5xx responses.
	// Defaults to 3.
	MaxRetries int
}

// client is the concrete S3Uploader implementation.
type client struct {
	cfg        Config
	httpClient *http.Client
}

// New constructs a real S3Uploader from the given Config.
func New(cfg Config) S3Uploader {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	return &client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// PutObject uploads body to S3 with exponential backoff on 5xx.
func (c *client) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	objectURL := c.objectURL(key)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("s3: build request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)

		if err := c.signRequest(req, body); err != nil {
			return fmt.Errorf("s3: sign request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("s3: http: %w", err)
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("s3: server error %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("s3: client error %d", resp.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("s3: PutObject failed after %d attempts: %w", c.cfg.MaxRetries+1, lastErr)
}

// PresignURL returns a presigned GET URL using AWS Signature V4 query signing.
func (c *client) PresignURL(_ context.Context, key string, ttl time.Duration) (PresignedURL, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	date := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	credScope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	credential := c.cfg.AccessKeyID + "/" + credScope

	objectURL := c.objectURL(key)
	u, err := url.Parse(objectURL)
	if err != nil {
		return PresignedURL{}, fmt.Errorf("s3: parse url: %w", err)
	}

	ttlSeconds := int(math.Round(ttl.Seconds()))

	q := u.Query()
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", ttlSeconds))
	q.Set("X-Amz-SignedHeaders", "host")
	u.RawQuery = q.Encode()

	// Canonical request.
	host := u.Host
	canonicalHeaders := "host:" + host + "\n"
	signedHeaders := "host"
	payloadHash := "UNSIGNED-PAYLOAD"

	// Sort query params for canonical query string.
	keys := make([]string, 0)
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(q.Get(k)))
	}
	canonicalQueryString := strings.Join(parts, "&")

	canonicalRequest := strings.Join([]string{
		"GET",
		u.EscapedPath(),
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// String to sign.
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credScope,
		hex.EncodeToString(hashSHA256([]byte(canonicalRequest))),
	}, "\n")

	// Signing key.
	signingKey := deriveSigningKey(c.cfg.SecretAccessKey, date, c.cfg.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	q.Set("X-Amz-Signature", signature)
	u.RawQuery = q.Encode()

	return PresignedURL{URL: u.String(), ExpiresAt: expiresAt}, nil
}

// objectURL builds the full S3 object URL.
func (c *client) objectURL(key string) string {
	if c.cfg.Endpoint != "" {
		return strings.TrimRight(c.cfg.Endpoint, "/") + "/" + c.cfg.Bucket + "/" + key
	}
	return "https://" + c.cfg.Bucket + ".s3." + c.cfg.Region + ".amazonaws.com/" + key
}

// signRequest adds AWS Signature V4 Authorization header to the request.
func (c *client) signRequest(req *http.Request, body []byte) error {
	now := time.Now().UTC()
	date := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", hex.EncodeToString(hashSHA256(body)))

	host := req.URL.Host
	req.Header.Set("host", host)

	// Collect signed headers (sorted).
	signedHeadersList := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(signedHeadersList)
	signedHeaders := strings.Join(signedHeadersList, ";")

	canonicalHeaders := ""
	for _, h := range signedHeadersList {
		canonicalHeaders += h + ":" + strings.TrimSpace(req.Header.Get(h)) + "\n"
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		hex.EncodeToString(hashSHA256(body)),
	}, "\n")

	credScope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credScope,
		hex.EncodeToString(hashSHA256([]byte(canonicalRequest))),
	}, "\n")

	signingKey := deriveSigningKey(c.cfg.SecretAccessKey, date, c.cfg.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	credential := c.cfg.AccessKeyID + "/" + credScope
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s, SignedHeaders=%s, Signature=%s",
		credential, signedHeaders, signature,
	))
	return nil
}

// deriveSigningKey produces the AWS4 signing key.
func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func hashSHA256(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

// drain discards the response body to allow connection reuse.
func drain(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
