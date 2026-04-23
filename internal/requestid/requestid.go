package requestid

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net"
	"regexp"
	"time"
)

const (
	// ContextKey is the key used in context.Context and gin.Context.
	ContextKey = "request_id"
	// HeaderName is the canonical inbound/outbound HTTP header.
	HeaderName = "X-Request-ID"
	// JobIDKey is the key used in context.Context for background jobs.
	JobIDKey = "job_id"
	// WorkerIDKey is the key used in context.Context for the worker identity.
	WorkerIDKey = "worker_id"
)

// Unexported typed context keys to avoid collisions with other packages.
type requestIDCtxKey struct{}
type jobIDCtxKey struct{}
type workerIDCtxKey struct{}

// validIDPattern matches only ASCII alphanumeric characters and '-', '_', '.'.
var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9\-_.]+$`)

// Generate returns a cryptographically random 24-hex-character ID.
// Falls back to a time-based ID if crypto/rand fails.
func Generate() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fallback: encode UnixNano as hex (8 bytes = 16 hex chars, pad to 24)
		nano := time.Now().UnixNano()
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(nano))
		// Pad to 12 bytes by repeating the nano bytes
		copy(b[:8], buf[:])
		copy(b[8:], buf[:4])
	}
	return hex.EncodeToString(b)
}

// Sanitize validates an inbound ID value.
// Accepts only [a-zA-Z0-9\-_.], max 128 chars, rejects empty or whitespace-only.
// Returns (value, true) if valid, ("", false) if not.
// Leading/trailing whitespace causes rejection — values are NOT trimmed before checking.
func Sanitize(value string) (string, bool) {
	if len(value) == 0 || len(value) > 128 {
		return "", false
	}
	if !validIDPattern.MatchString(value) {
		return "", false
	}
	return value, true
}

// IsTrustedSource returns true if remoteAddr falls within one of the provided CIDR ranges.
// Returns false if the allowlist is empty or the IP cannot be parsed.
// remoteAddr may be in "host:port" or plain "host" format.
func IsTrustedSource(remoteAddr string, allowlist []net.IPNet) bool {
	if len(allowlist) == 0 {
		return false
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Not "host:port" format — try parsing as plain IP
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, cidr := range allowlist {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// WithRequestID returns a context carrying the given request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

// FromContext extracts the request ID from ctx. Returns ("", false) if absent.
func FromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDCtxKey{}).(string)
	return id, ok && id != ""
}

// WithJobID returns a context carrying the given job ID.
func WithJobID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, jobIDCtxKey{}, id)
}

// JobIDFromContext extracts the job ID from ctx. Returns ("", false) if absent.
func JobIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(jobIDCtxKey{}).(string)
	return id, ok && id != ""
}

// WithWorkerID returns a context carrying the worker ID.
func WithWorkerID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workerIDCtxKey{}, id)
}

// WorkerIDFromContext extracts the worker ID from ctx. Returns ("", false) if absent.
func WorkerIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(workerIDCtxKey{}).(string)
	return id, ok && id != ""
}
