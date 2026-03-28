package secrets

const redacted = "***REDACTED***"

// SafeValue wraps a secret string so that it is never accidentally logged or serialized.
// Use Expose() to intentionally access the plaintext.
type SafeValue struct {
	inner string
}

// NewSafeValue wraps a plaintext secret.
func NewSafeValue(plaintext string) SafeValue {
	return SafeValue{inner: plaintext}
}

// Expose returns the plaintext secret. Call this only when you intentionally
// need the raw value (e.g. passing to a crypto function or database driver).
func (s SafeValue) Expose() string {
	return s.inner
}

// String implements fmt.Stringer and always returns a redacted placeholder.
func (s SafeValue) String() string {
	return redacted
}

// GoString implements fmt.GoStringer for %#v formatting.
func (s SafeValue) GoString() string {
	return "SafeValue(" + redacted + ")"
}

// MarshalJSON ensures the secret is redacted if accidentally marshalled to JSON.
func (s SafeValue) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

// MarshalText ensures the secret is redacted if accidentally marshalled to text.
func (s SafeValue) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// IsEmpty returns true if the underlying secret is empty.
func (s SafeValue) IsEmpty() bool {
	return s.inner == ""
}
