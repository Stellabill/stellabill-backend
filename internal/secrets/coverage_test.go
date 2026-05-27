package secrets

import "testing"

func TestCoverage_SafeValue_MarshalText(t *testing.T) {
	sv := NewSafeValue("plain")
	b, err := sv.MarshalText()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if string(b) == "" {
		t.Fatal("expected redacted output")
	}
}
