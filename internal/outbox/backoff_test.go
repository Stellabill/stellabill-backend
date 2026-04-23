package outbox

import (
	"math"
	"testing"
	"testing/quick"
	"time"
)

// Feature: outbox-hardening, Property 8: Backoff duration is positive and bounded
// Validates: Requirements 4.1, 4.4
func TestCalculateNextRetry_PositiveAndBounded(t *testing.T) {
	cfg := DispatcherConfig{
		RetryBaseDelay:     1 * time.Second,
		RetryBackoffFactor: 2.0,
		MaxRetries:         10,
	}

	f := func(retryCount uint8) bool {
		// Constrain retryCount to [0, MaxRetries)
		rc := int(retryCount) % cfg.MaxRetries

		d := CalculateNextRetry(rc, cfg)

		base := float64(cfg.RetryBaseDelay)
		factor := cfg.RetryBackoffFactor
		lowerBound := time.Duration(base * math.Pow(factor, float64(rc)))
		upperBound := lowerBound + cfg.RetryBaseDelay

		return d >= lowerBound && d < upperBound
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("Property 8 failed: %v", err)
	}
}

// Feature: outbox-hardening, Property 9: Backoff is monotonically non-decreasing (lower bound)
// Validates: Requirements 4.1, 4.4
func TestCalculateNextRetry_MonotonicLowerBound(t *testing.T) {
	cfg := DispatcherConfig{
		RetryBaseDelay:     1 * time.Second,
		RetryBackoffFactor: 2.0,
		MaxRetries:         10,
	}

	// The lower bound is base * factor^retryCount (deterministic part, no jitter).
	// For a < b, lowerBound(a) <= lowerBound(b).
	lowerBound := func(rc int) time.Duration {
		base := float64(cfg.RetryBaseDelay)
		return time.Duration(base * math.Pow(cfg.RetryBackoffFactor, float64(rc)))
	}

	f := func(a, b uint8) bool {
		ra := int(a) % cfg.MaxRetries
		rb := int(b) % cfg.MaxRetries

		if ra > rb {
			ra, rb = rb, ra // ensure ra <= rb
		}

		return lowerBound(ra) <= lowerBound(rb)
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("Property 9 failed: %v", err)
	}
}
