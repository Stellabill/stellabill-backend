package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Hash
// ---------------------------------------------------------------------------

func TestHash_ProducesNonEmptyEncoded(t *testing.T) {
	encoded, err := Hash("correcthorsebatterystaple")
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)
}

func TestHash_EncodedHasExpectedPrefix(t *testing.T) {
	encoded, err := Hash("password")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encoded, "$argon2id$v1$"), "encoded=%q", encoded)
}

func TestHash_UniquePerCall(t *testing.T) {
	// Two invocations with the same password must produce different encoded
	// strings because each call draws a fresh random salt.
	a, err := Hash("same-password")
	require.NoError(t, err)

	b, err := Hash("same-password")
	require.NoError(t, err)

	assert.NotEqual(t, a, b, "Hash must use a unique salt on every call")
}

func TestHash_EmptyPassword(t *testing.T) {
	// Empty strings are valid inputs to Argon2id; we must not special-case them.
	encoded, err := Hash("")
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)
}

func TestHash_LongPassword(t *testing.T) {
	// 1024-byte password: Argon2id imposes no length limit.
	long := strings.Repeat("x", 1024)
	encoded, err := Hash(long)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)
}

func TestHash_UnicodePassword(t *testing.T) {
	encoded, err := Hash("pässwörд 🔑")
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)
}

// ---------------------------------------------------------------------------
// Verify — happy path
// ---------------------------------------------------------------------------

func TestVerify_CorrectPassword(t *testing.T) {
	password := "correcthorsebatterystaple"
	encoded, err := Hash(password)
	require.NoError(t, err)

	err = Verify(password, encoded)
	assert.NoError(t, err)
}

func TestVerify_EmptyPassword(t *testing.T) {
	encoded, err := Hash("")
	require.NoError(t, err)

	assert.NoError(t, Verify("", encoded))
}

func TestVerify_UnicodePassword(t *testing.T) {
	pw := "pässwörд 🔑"
	encoded, err := Hash(pw)
	require.NoError(t, err)
	assert.NoError(t, Verify(pw, encoded))
}

// ---------------------------------------------------------------------------
// Verify — wrong password
// ---------------------------------------------------------------------------

func TestVerify_WrongPassword_ReturnsMismatch(t *testing.T) {
	encoded, err := Hash("correct")
	require.NoError(t, err)

	err = Verify("wrong", encoded)
	assert.ErrorIs(t, err, ErrMismatch)
}

func TestVerify_EmptyVsNonEmpty_ReturnsMismatch(t *testing.T) {
	encoded, err := Hash("nonempty")
	require.NoError(t, err)
	assert.ErrorIs(t, Verify("", encoded), ErrMismatch)
}

func TestVerify_NonEmptyVsEmpty_ReturnsMismatch(t *testing.T) {
	encoded, err := Hash("")
	require.NoError(t, err)
	assert.ErrorIs(t, Verify("nonempty", encoded), ErrMismatch)
}

func TestVerify_CaseSensitive(t *testing.T) {
	encoded, err := Hash("Password")
	require.NoError(t, err)
	assert.ErrorIs(t, Verify("password", encoded), ErrMismatch)
}

// ---------------------------------------------------------------------------
// Verify — malformed / corrupt encoded strings (must not panic)
// ---------------------------------------------------------------------------

func TestVerify_CompletelyEmpty_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", ""), ErrInvalidHash)
}

func TestVerify_RandomGarbage_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "not-a-hash-at-all"), ErrInvalidHash)
}

func TestVerify_TooFewSegments_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v1$m=65536,t=3,p=4$onlyfivesegments"), ErrInvalidHash)
}

func TestVerify_WrongAlgorithmLabel_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$bcrypt$v1$m=65536,t=3,p=4$abc$def"), ErrInvalidHash)
}

func TestVerify_WrongVersion_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v9$m=65536,t=3,p=4$abc$def"), ErrInvalidHash)
}

func TestVerify_MalformedParams_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v1$NOPE$abc$def"), ErrInvalidHash)
}

func TestVerify_InvalidBase64Salt_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v1$m=65536,t=3,p=4$!!!invalid!!!$def"), ErrInvalidHash)
}

func TestVerify_InvalidBase64Hash_ReturnsInvalidHash(t *testing.T) {
	// Valid salt but corrupt hash field.
	encoded, err := Hash("pw")
	require.NoError(t, err)

	parts := strings.Split(encoded, "$")
	require.Len(t, parts, 6)
	parts[5] = "!!!invalid!!!"
	assert.ErrorIs(t, Verify("pw", strings.Join(parts, "$")), ErrInvalidHash)
}

func TestVerify_EmptySaltField_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v1$m=65536,t=3,p=4$$YWJj"), ErrInvalidHash)
}

func TestVerify_EmptyHashField_ReturnsInvalidHash(t *testing.T) {
	assert.ErrorIs(t, Verify("pw", "$argon2id$v1$m=65536,t=3,p=4$YWJj$"), ErrInvalidHash)
}

func TestVerify_CorruptHashBytes_ReturnsMismatch(t *testing.T) {
	// Produce a valid encoded hash, then flip a byte in the stored hash
	// segment.  Verify must return ErrMismatch, not panic or ErrInvalidHash.
	encoded, err := Hash("password")
	require.NoError(t, err)

	parts := strings.Split(encoded, "$")
	require.Len(t, parts, 6)

	// Flip the first character of the base64 hash to a different valid b64
	// character so the base64 decode still succeeds but the hash diverges.
	b := []byte(parts[5])
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	parts[5] = string(b)

	assert.ErrorIs(t, Verify("password", strings.Join(parts, "$")), ErrMismatch)
}

// ---------------------------------------------------------------------------
// Error sentinel identity
// ---------------------------------------------------------------------------

func TestErrMismatch_IsDistinctFromErrInvalidHash(t *testing.T) {
	assert.NotEqual(t, ErrMismatch, ErrInvalidHash)
	assert.False(t, strings.EqualFold(ErrMismatch.Error(), ErrInvalidHash.Error()))
}

// ---------------------------------------------------------------------------
// Encode round-trip — structural validation
// ---------------------------------------------------------------------------

func TestHash_EncodedFieldCount(t *testing.T) {
	encoded, err := Hash("test")
	require.NoError(t, err)

	// $argon2id$v1$m=…$<salt>$<hash> → split on "$" → 6 parts (first is "")
	parts := strings.Split(encoded, "$")
	assert.Len(t, parts, 6, "encoded=%q", encoded)
	assert.Equal(t, "argon2id", parts[1])
	assert.Equal(t, encodingVersion, parts[2])
}

func TestHash_ParametersMatchConstants(t *testing.T) {
	encoded, err := Hash("test")
	require.NoError(t, err)

	var m, iters uint32
	var p uint8
	parts := strings.Split(encoded, "$")
	require.Len(t, parts, 6)

	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &iters, &p)
	require.NoError(t, err)

	assert.Equal(t, argonMemory, m, "memory parameter must match constant")
	assert.Equal(t, argonIterations, iters, "iterations parameter must match constant")
	assert.Equal(t, argonParallelism, p, "parallelism parameter must match constant")
}
