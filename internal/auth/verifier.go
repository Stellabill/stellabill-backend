package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// TokenVerifier defines the interface for verifying authentication tokens.
type TokenVerifier interface {
	Verify(ctx context.Context, tokenString string) (*Claims, error)
}

// keyCache is the subset of JWKSCache used for key lookup.
type keyCache interface {
	Get(ctx context.Context, kid string) (*rsa.PublicKey, error)
	Refresh(ctx context.Context) error
}

// jwtHeader represents the protected header of a JWT.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// HMACVerifier verifies HS256 tokens using a static secret.
type HMACVerifier struct {
	secret []byte
}

// NewHMACVerifier creates a TokenVerifier for symmetric signatures.
func NewHMACVerifier(secret string) *HMACVerifier {
	return &HMACVerifier{secret: []byte(secret)}
}

// Verify validates an HS256 token.
func (v *HMACVerifier) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token: not a JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if header.Alg != "HS256" {
		return nil, fmt.Errorf("unexpected signing method: %s", header.Alg)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if validator, ok := interface{}(&claims).(interface{ Valid() error }); ok {
		if err := validator.Valid(); err != nil {
			return nil, err
		}
	}
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if !hmac.Equal(sig, expected) {
		return nil, errors.New("invalid token signature")
	}
	return &claims, nil
}

// JWKSVerifier verifies RS256 tokens using a JWKS cache.
type JWKSVerifier struct {
	cache keyCache
}

// NewJWKSVerifier creates a TokenVerifier backed by a JWKS cache.
func NewJWKSVerifier(cache keyCache) *JWKSVerifier {
	return &JWKSVerifier{cache: cache}
}

// Verify parses and validates a JWT, selecting the public key by kid.
func (v *JWKSVerifier) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token: not a JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unexpected signing method: %s", header.Alg)
	}
	if header.Kid == "" {
		return nil, errors.New("token missing kid header")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if validator, ok := interface{}(&claims).(interface{ Valid() error }); ok {
		if err := validator.Valid(); err != nil {
			return nil, err
		}
	}
	key, err := v.cache.Get(ctx, header.Kid)
	if err != nil {
		// One forced refresh on cache miss avoids a hot loop.
		if rerr := v.cache.Refresh(ctx); rerr != nil {
			return nil, fmt.Errorf("key refresh failed: %w", rerr)
		}
		key, err = v.cache.Get(ctx, header.Kid)
		if err != nil {
			return nil, fmt.Errorf("key %q not found after refresh: %w", header.Kid, err)
		}
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	signed := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signed)
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, fmt.Errorf("verify signature: %w", err)
	}
	return &claims, nil
}

// NewVerifier returns a TokenVerifier based on configuration.
// If jwksURL is empty, HMAC verification is used; otherwise JWKS is used.
func NewVerifier(secret, jwksURL string, cache keyCache) TokenVerifier {
	if jwksURL == "" {
		return NewHMACVerifier(secret)
	}
	return NewJWKSVerifier(cache)
}

var _ TokenVerifier = (*HMACVerifier)(nil)
var _ TokenVerifier = (*JWKSVerifier)(nil)
