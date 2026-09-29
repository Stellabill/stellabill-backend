package security

import (
	"testing"
)

// BenchmarkHash measures the wall-clock time of a single Hash call using the
// package-level cost parameters.  Run it on your deployment hardware to verify
// that each invocation takes at least 100 ms — the OWASP minimum for
// interactive password hashing — before accepting the parameters as adequate.
//
// Usage:
//
//	go test -bench=BenchmarkHash -benchtime=5s ./internal/security/
//
// If the result falls below 100 ms/op on your target hardware, increase
// argonMemory or argonIterations in passhash.go.  Do NOT weaken parameters
// under any build tag.
func BenchmarkHash(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := Hash("benchmark-password-string"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerify measures the cost of verifying a correct password.
// Verification timing should be essentially identical to hashing because
// Argon2id runs the full KDF in both directions.
func BenchmarkVerify(b *testing.B) {
	encoded, err := Hash("benchmark-password-string")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := Verify("benchmark-password-string", encoded); err != nil {
			b.Fatal(err)
		}
	}
}

// TestBenchmarkHash_MinLatency is a regular test (not a benchmark) that
// enforces a lower-bound on hashing time.  It catches accidental parameter
// weakening — e.g. a refactor that reduces memory or iterations — before it
// reaches production.
//
// The threshold is intentionally generous (50 ms) to remain green across
// slow CI runners.  On production hardware the real value should be ≥ 100 ms.
//
// This test will FAIL if the cost parameters are weakened.
func TestBenchmarkHash_MinLatency(t *testing.T) {
	const (
		rounds        = 3
		minPerHash_ms = 50 // generous CI floor; production target is ≥ 100 ms
	)

	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Hash("latency-sentinel"); err != nil {
				b.Fatal(err)
			}
		}
	})

	nsPerOp := result.NsPerOp()
	msPerOp := nsPerOp / 1_000_000

	_ = rounds
	if msPerOp < int64(minPerHash_ms) {
		t.Errorf(
			"Hash is too fast: %d ms/op — cost parameters may have been weakened.\n"+
				"OWASP target: ≥ 100 ms on production hardware, ≥ %d ms on CI.\n"+
				"Review argonMemory and argonIterations in passhash.go.",
			msPerOp, minPerHash_ms,
		)
	} else {
		t.Logf("Hash latency: %d ms/op (threshold: ≥ %d ms) ✓", msPerOp, minPerHash_ms)
	}
}
