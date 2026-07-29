package outbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
)

// SigningKeyProvider abstracts access to the Ed25519 signing key used for
// outbox payload signing. The key is identified by a key ID (kid) used for
// rotation — the provider returns the current active signing key.
type SigningKeyProvider interface {
	// SigningKey returns the current Ed25519 private key (in JWK format) and
	// its key ID. Implementations should manage rotation transparently so the
	// signer always uses the latest active key.
	SigningKey(ctx context.Context) (privateJWK jwk.Key, kid string, err error)
}

// envSigningKeyProvider reads a base64-encoded Ed25519 private key from the
// OUTBOX_SIGNING_KEY environment variable. The key should be the raw 64-byte
// seed of an Ed25519 private key, base64url-encoded (no padding).
//
// This provider is suitable for development and statically-configured
// deployments. Production deployments should use the secrets.ChainProvider or
// a Vault-backed implementation that supports key rotation.
type envSigningKeyProvider struct {
	once sync.Once
	key  jwk.Key
	kid  string
	err  error
}

// NewEnvSigningKeyProvider returns a SigningKeyProvider that reads the signing
// key from the OUTBOX_SIGNING_KEY environment variable. When the variable is
// empty, an ephemeral key is generated (suitable for development only).
func NewEnvSigningKeyProvider() SigningKeyProvider {
	return &envSigningKeyProvider{}
}

func (p *envSigningKeyProvider) SigningKey(_ context.Context) (jwk.Key, string, error) {
	p.once.Do(p.load)
	return p.key, p.kid, p.err
}

func (p *envSigningKeyProvider) load() {
	encoded := os.Getenv("OUTBOX_SIGNING_KEY")
	if encoded == "" {
		// No key configured — generate an ephemeral key for development.
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			p.err = fmt.Errorf("generate ephemeral Ed25519 key: %w", err)
			return
		}
		key, err := jwk.FromRaw(priv)
		if err != nil {
			p.err = fmt.Errorf("jwk from raw: %w", err)
			return
		}
		kid := fmt.Sprintf("ephemeral-%d", time.Now().Unix())
		if err := key.Set(jwk.KeyIDKey, kid); err != nil {
			p.err = fmt.Errorf("set kid: %w", err)
			return
		}
		p.key = key
		p.kid = kid
		return
	}

	seed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		p.err = fmt.Errorf("decode OUTBOX_SIGNING_KEY: %w", err)
		return
	}
	if len(seed) != ed25519.SeedSize {
		p.err = fmt.Errorf("OUTBOX_SIGNING_KEY: expected %d bytes, got %d", ed25519.SeedSize, len(seed))
		return
	}

	priv := ed25519.NewKeyFromSeed(seed)
	key, err := jwk.FromRaw(priv)
	if err != nil {
		p.err = fmt.Errorf("jwk from raw: %w", err)
		return
	}
	// Derive a stable kid from the public key hash.
	pubHash := sha256.Sum256(priv.Public().(ed25519.PublicKey))
	kid := fmt.Sprintf("k-%x", pubHash[:8])
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		p.err = fmt.Errorf("set kid: %w", err)
		return
	}
	p.key = key
	p.kid = kid
}

// Ed25519Signer implements the Publisher interface by signing each event's
// payload with an Ed25519 key before delegating to the inner publisher.
// Consumers can verify the signature using the published JWKS at
// /.well-known/outbox-jwks.json.
//
// The signature is attached as a JWS compact serialization alongside the
// original payload in the event's EventData, so consumers can verify without
// a separate round trip.
type Ed25519Signer struct {
	inner   Publisher
	keyProv SigningKeyProvider
}

// NewEd25519Signer creates a Publisher that signs outbox payloads with the
// Ed25519 key provided by keyProv. When keyProv is nil, an ephemeral key is
// generated from the environment variable OUTBOX_SIGNING_KEY (or an ephemeral
// key for development).
func NewEd25519Signer(inner Publisher, keyProv SigningKeyProvider) Publisher {
	if keyProv == nil {
		keyProv = NewEnvSigningKeyProvider()
	}
	return &Ed25519Signer{
		inner:   inner,
		keyProv: keyProv,
	}
}

// SignedEventData wraps the original payload with a JWS compact signature so
// consumers can verify authenticity and integrity before processing.
type SignedEventData struct {
	Type         string      `json:"type"`
	Data         interface{} `json:"data,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
	ID           string      `json:"id"`
	Encrypted    bool        `json:"encrypted,omitempty"`
	JWE          string      `json:"jwe,omitempty"`
	KeyID        string      `json:"key_id,omitempty"`
	SubscriberID string      `json:"subscriber_id,omitempty"`

	// Signed is set to true when the payload carries a JWS signature.
	Signed bool `json:"signed,omitempty"`
	// Signature is the JWS compact serialization of the original payload.
	Signature string `json:"signature,omitempty"`
	// SigningKeyID identifies the key used to sign (for rotation).
	SigningKeyID string `json:"signing_key_id,omitempty"`
}

// Publish signs the event payload and delegates to the inner publisher.
func (s *Ed25519Signer) Publish(ctx context.Context, event *Event) error {
	key, kid, err := s.keyProv.SigningKey(ctx)
	if err != nil {
		return fmt.Errorf("signing key unavailable: %w", err)
	}

	var envelope EventData
	if err := json.Unmarshal(event.EventData, &envelope); err != nil {
		return fmt.Errorf("unmarshal event data: %w", err)
	}

	// Serialize the payload we want to sign.
	payload := map[string]interface{}{
		"id":             event.ID,
		"type":           event.EventType,
		"data":           envelope.Data,
		"occurred_at":    event.OccurredAt,
		"aggregate_id":   event.AggregateID,
		"aggregate_type": event.AggregateType,
		"version":        event.Version,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal signing payload: %w", err)
	}

	// Sign with Ed25519 (EdDSA) using JWS compact serialization.
	headers := jws.NewHeaders()
	if err := headers.Set(jws.KeyIDKey, kid); err != nil {
		return fmt.Errorf("set kid header: %w", err)
	}

	signed, err := jws.Sign(payloadBytes, jws.WithKey(jwa.EdDSA, key, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return fmt.Errorf("jws sign: %w", err)
	}

	// Build the signed envelope.
	signedEnvelope := SignedEventData{
		Type:         envelope.Type,
		Data:         envelope.Data,
		Timestamp:    envelope.Timestamp,
		ID:           envelope.ID,
		Encrypted:    envelope.Encrypted,
		JWE:          envelope.JWE,
		KeyID:        envelope.KeyID,
		SubscriberID: envelope.SubscriberID,
		Signed:       true,
		Signature:    string(signed),
		SigningKeyID: kid,
	}

	signedJSON, err := json.Marshal(signedEnvelope)
	if err != nil {
		return fmt.Errorf("marshal signed envelope: %w", err)
	}

	signedEvent := *event
	signedEvent.EventData = json.RawMessage(signedJSON)
	return s.inner.Publish(ctx, &signedEvent)
}

// VerifyOutboxSignature verifies a JWS compact signature on a received outbox
// event. It returns the verified payload bytes when the signature is valid, or
// an error describing why verification failed.
//
// Consumers should call this function before processing an event. Verification
// failure should result in the event being routed to the dead-letter queue
// (DLQ) with the verification error as the reason.
func VerifyOutboxSignature(signedEventData []byte, jwksKey jwk.Key) ([]byte, error) {
	var envelope SignedEventData
	if err := json.Unmarshal(signedEventData, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal signed event data: %w", err)
	}

	if !envelope.Signed || envelope.Signature == "" {
		return nil, errors.New("event is not signed")
	}

	payload, err := jws.Verify([]byte(envelope.Signature), jws.WithKey(jwa.EdDSA, jwksKey))
	if err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return payload, nil
}

// PublicJWKFromSigningKey derives a JWK containing the public portion of the
// signing key. This can be served at /.well-known/outbox-jwks.json so that
// consumers can fetch the public key for signature verification.
func PublicJWKFromSigningKey(privateJWK jwk.Key) (jwk.Key, error) {
	var pub interface{}
	if err := privateJWK.Raw(&pub); err != nil {
		return nil, fmt.Errorf("raw key: %w", err)
	}

	edPriv, ok := pub.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("key is not an Ed25519 private key")
	}

	pubJWK, err := jwk.FromRaw(edPriv.Public())
	if err != nil {
		return nil, fmt.Errorf("jwk from public key: %w", err)
	}

	// Copy the kid from the private key.
	if kid, ok := privateJWK.KeyID(); ok && kid != "" {
		if err := pubJWK.Set(jwk.KeyIDKey, kid); err != nil {
			return nil, fmt.Errorf("set kid on public JWK: %w", err)
		}
	}

	return pubJWK, nil
}
