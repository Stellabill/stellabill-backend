package outbox

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwe"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// ErrMissingSubscriberKey indicates no usable subscriber key was found.
var ErrMissingSubscriberKey = errors.New("missing subscriber encryption key")

// PermanentPublishError marks publish failures that should not be retried.
type PermanentPublishError struct {
	Reason string
	Err    error
}

func (e *PermanentPublishError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Reason, e.Err)
	}
	return e.Reason
}

func (e *PermanentPublishError) Unwrap() error {
	return e.Err
}

// IsPermanentPublishError reports whether err should bypass retry and go to DLQ.
func IsPermanentPublishError(err error) bool {
	var target *PermanentPublishError
	return errors.As(err, &target)
}

// JWEEncryptor encrypts payloads using subscriber-supplied public JWKs.
type JWEEncryptor struct{}

// NewJWEEncryptor creates a JWE encryptor.
func NewJWEEncryptor() *JWEEncryptor {
	return &JWEEncryptor{}
}

// Encrypt serializes payload and returns a compact JWE string.
func (e *JWEEncryptor) Encrypt(payload interface{}, publicJWK json.RawMessage) (string, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	key, err := jwk.ParseKey(publicJWK)
	if err != nil {
		return "", fmt.Errorf("parse JWK: %w", err)
	}

	encrypted, err := jwe.Encrypt(
		plaintext,
		jwe.WithKey(jwa.RSA_OAEP_256, key),
		jwe.WithContentEncryption(jwa.A256GCM),
	)
	if err != nil {
		return "", fmt.Errorf("encrypt payload: %w", err)
	}
	return string(encrypted), nil
}

// DecryptForTest decrypts a compact JWE using a private JWK (tests only).
func DecryptForTest(compactJWE string, privateJWK json.RawMessage) ([]byte, error) {
	key, err := jwk.ParseKey(privateJWK)
	if err != nil {
		return nil, fmt.Errorf("parse private JWK: %w", err)
	}
	return jwe.Decrypt([]byte(compactJWE), jwe.WithKey(jwa.RSA_OAEP_256, key))
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
