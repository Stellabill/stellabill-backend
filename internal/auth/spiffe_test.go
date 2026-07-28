package auth

import (
	"context"
	"testing"
)

func TestSpiffeVerifier_DisabledInDev(t *testing.T) {
	v, err := NewSpiffeVerifier(context.Background(), "", "example.org", "development")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err = v.Verify(context.Background(), "token")
	if err == nil || err.Error() != "SPIFFE verifier is disabled" {
		t.Errorf("expected disabled error, got %v", err)
	}
}

func TestSpiffeVerifier_MissingSocketProd(t *testing.T) {
	_, err := NewSpiffeVerifier(context.Background(), "", "example.org", "production")
	if err == nil {
		t.Fatal("expected error for missing socket in production")
	}
}

func TestSpiffeVerifier_InvalidTrustDomain(t *testing.T) {
	_, err := NewSpiffeVerifier(context.Background(), "/tmp/socket", "invalid trust domain", "production")
	if err == nil {
		t.Fatal("expected error for invalid trust domain")
	}
}
