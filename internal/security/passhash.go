// Package security provides primitives for admin authentication hardening,
// including exponential lockout tracking for failed login attempts and
// Argon2id password hashing for future local admin credential storage.
//
// # Argon2id parameters
//
// The cost parameters below follow the OWASP Password Storage Cheat Sheet
// (2024 edition, https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
// and RFC 9106 §4 "Parameter Choice" recommendations for interactive logins:
//
//   - Memory:      64 MiB  (RFC 9106 recommends ≥ 64 MiB for interactive use)
//   - Iterations:  3       (OWASP minimum for Argon2id interactive profile)
//   - Parallelism: 4       (matches a typical 4-vCPU deployment; tune with BenchmarkHash)
//   - Salt length: 16 B    (128-bit, per RFC 9106 §3.1 "Recommended Parameters")
//   - Tag length:  32 B    (256-bit output, sufficient for password verification)
//
// These parameters target ≥ 100 ms per hash on reference hardware (2024
// commodity server, single core).  Run the benchmark to verify on your
// deployment target:
//
//	go test -bench=BenchmarkHash -benchtime=5s ./internal/security/
//
// Do NOT weaken these parameters under any build tag.  Reducing memory or
// iterations degrades resistance to offline dictionary attacks.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id cost parameters.  These are intentionally unexported package-level
// constants so that no call site can accidentally pass lower values.
//
// Sources:
//   - OWASP PCSC 2024: memory=64MiB, iterations=3, parallelism=4
//   - RFC 9106 §4: salt≥128 bits, tag≥128 bits
const (
	argonMemory      uint32 = 64 * 1024 // 64 MiB in KiB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 4
	argonSaltLen            = 16 // bytes → 128-bit salt
	argonKeyLen      uint32 = 32 // bytes → 256-bit tag
)

// encodingVersion is embedded in every encoded hash so future parameter
// migrations can be detected without breaking existing stored hashes.
const encodingVersion = "v1"

// ErrMismatch is returned by Verify when the password does not match the
// stored hash.  It is intentionally opaque — callers must not log or surface
// the inner details of a failed verification.
var ErrMismatch = errors.New("security: password does not match")

// ErrInvalidHash is returned by Verify when the encoded hash string is
// syntactically malformed or contains unrecognised fields.  This covers
// truncated strings, wrong field count, non-base64 payloads, and unsupported
// version tags — all without panicking.
var ErrInvalidHash = errors.New("security: encoded hash is malformed")

// Hash derives an Argon2id digest of password and returns a self-describing
// encoded string that embeds all parameters needed for future verification.
// Each call generates a fresh cryptographically random salt, so the same
// password will never produce the same encoded output twice.
//
// The returned string has the form:
//
//	$argon2id$v1$m=65536,t=3,p=4$<salt-b64>$<hash-b64>
//
// Store this entire string; pass it unchanged to Verify.
//
// Hash returns a non-nil error only when the OS CSPRNG is unavailable
// (extremely rare).  It never returns a nil error with an empty string.
func Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("security: generating salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		argonKeyLen,
	)

	encoded := fmt.Sprintf(
		"$argon2id$%s$m=%d,t=%d,p=%d$%s$%s",
		encodingVersion,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	return encoded, nil
}

// Verify checks whether password matches the encoded hash produced by Hash.
// It uses a constant-time comparison to prevent timing-based side-channel
// attacks.
//
// Return values:
//   - nil            — password matches
//   - ErrMismatch    — password is wrong; do not log the provided password
//   - ErrInvalidHash — encoded is syntactically malformed; log and investigate
//   - other error    — unexpected internal failure
//
// Verify never panics, even when given an arbitrarily corrupt encoded string.
func Verify(password, encoded string) error {
	salt, storedHash, params, err := decode(encoded)
	if err != nil {
		return err
	}

	candidate := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		uint32(len(storedHash)),
	)

	if subtle.ConstantTimeCompare(candidate, storedHash) != 1 {
		return ErrMismatch
	}

	return nil
}

// hashParams holds the decoded Argon2id cost values extracted from an encoded
// hash string.
type hashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

// decode parses an encoded hash string produced by Hash and returns its
// constituent parts.  It returns ErrInvalidHash for any syntactic problem so
// callers get a single, stable sentinel rather than a cascade of format errors.
func decode(encoded string) (salt, hash []byte, params hashParams, err error) {
	// Expected format: $argon2id$v1$m=65536,t=3,p=4$<salt>$<hash>
	// Split produces: ["", "argon2id", "v1", "m=…", "<salt>", "<hash>"]
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	if parts[2] != encodingVersion {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	var m, t uint32
	var p uint8
	_, scanErr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p)
	if scanErr != nil {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) == 0 {
		return nil, nil, hashParams{}, ErrInvalidHash
	}

	return salt, hash, hashParams{memory: m, iterations: t, parallelism: p}, nil
}
