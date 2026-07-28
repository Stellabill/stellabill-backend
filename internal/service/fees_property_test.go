// Package service contains property-based tests for money calculation
// functions (ProrateFee, TaxSplit, RoundMoney) using pgregory.net/rapid.
//
// # Invariants tested
//
//   - ProrateFee: sum of parts equals the rounded input; no part is negative;
//     len(result) == parts.
//   - TaxSplit: tax + net equals the original amount; both components are
//     non-negative.
//   - RoundMoney: result has at most scale(currency) decimal places; is
//     idempotent (rounding an already-rounded value is a no-op).
//
// # Currency edge cases
//
//   - JPY (0 decimal places) — "zero-decimal" currency
//   - BHD (3 decimal places) — "high-decimal" currency
//   - USD (2 decimal places) — baseline
//
// # Counterexample archival
//
// When rapid finds a failing case it writes a JSON file under
// internal/service/testdata/. The file name encodes the test name and a
// timestamp so multiple failures never overwrite each other.
package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// saveCounterexample writes v as JSON to testdata/<testName>-<ts>.json.
// It is called inside rapid's t.Fatal path so failures are always archived.
func saveCounterexample(t *rapid.T, testName string, v any) {
	t.Helper()
	dir := "testdata"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	name := strings.ReplaceAll(testName, "/", "_")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", name, ts))
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// genCurrency draws one of the representative ISO 4217 currency codes used in
// the test suite, covering 0-, 2-, and 3-decimal currencies.
func genCurrency() *rapid.Generator[string] {
	currencies := []string{
		"USD", // 2 decimals — baseline
		"EUR", // 2 decimals
		"GBP", // 2 decimals
		"JPY", // 0 decimals — zero-decimal
		"KRW", // 0 decimals
		"BHD", // 3 decimals — high-decimal
		"KWD", // 3 decimals
		"OMR", // 3 decimals
	}
	return rapid.SampledFrom(currencies)
}

// genPositiveAmount generates a non-negative decimal in the range [0, 999999]
// with up to 6 decimal places, then quantised to the currency scale.
// The large upper bound exercises overflow-adjacent arithmetic.
func genPositiveAmount(currency string) *rapid.Generator[decimal.Decimal] {
	scale := scaleForCurrency(currency)
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		// Integer part: 0..999999
		units := rapid.Int64Range(0, 999999).Draw(t, "units")
		// Sub-unit part: 0..10^scale - 1
		var subunit int64
		if scale > 0 {
			maxSub := int64(1)
			for i := int32(0); i < scale; i++ {
				maxSub *= 10
			}
			subunit = rapid.Int64Range(0, maxSub-1).Draw(t, "subunit")
		}
		shift := decimal.NewFromInt(1)
		for i := int32(0); i < scale; i++ {
			shift = shift.Mul(decimal.NewFromInt(10))
		}
		d := decimal.NewFromInt(units).Add(
			decimal.NewFromInt(subunit).Div(shift),
		)
		return RoundMoney(d, currency)
	})
}

// genTaxRate draws a rate in [0, 1] with up to 4 decimal places.
func genTaxRate() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		// represent as integer basis points 0..10000  → divide by 10000
		bp := rapid.Int64Range(0, 10000).Draw(t, "basis_points")
		return decimal.NewFromInt(bp).Div(decimal.NewFromInt(10000))
	})
}

// genParts draws a number of proration parts in [1, 120].
// 120 covers monthly proration over 10 years (worst-case subscription split).
func genParts() *rapid.Generator[int] {
	return rapid.IntRange(1, 120)
}

// ---------------------------------------------------------------------------
// ProrateFee property tests
// ---------------------------------------------------------------------------

// TestPropProrateFee_SumEqualsInput asserts that the sum of all proration
// parts equals the rounded input amount for every (amount, currency, parts)
// triple that rapid can construct.
func TestPropProrateFee_SumEqualsInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")
		parts := genParts().Draw(t, "parts")

		result, err := ProrateFee(amount, currency, parts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sum := decimal.Zero
		for _, v := range result {
			sum = sum.Add(v)
		}

		want := RoundMoney(amount, currency)
		if !sum.Equal(want) {
			saveCounterexample(t, t.Name(), map[string]any{
				"currency": currency, "amount": amount.String(),
				"parts": parts, "sum": sum.String(), "want": want.String(),
			})
			t.Fatalf("sum=%s != want=%s (currency=%s, amount=%s, parts=%d)",
				sum, want, currency, amount, parts)
		}
	})
}

// TestPropProrateFee_NoNegativeParts asserts that no proration part is negative.
func TestPropProrateFee_NoNegativeParts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")
		parts := genParts().Draw(t, "parts")

		result, err := ProrateFee(amount, currency, parts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for i, v := range result {
			if v.IsNegative() {
				saveCounterexample(t, t.Name(), map[string]any{
					"currency": currency, "amount": amount.String(),
					"parts": parts, "part_index": i, "part_value": v.String(),
				})
				t.Fatalf("part[%d]=%s is negative (currency=%s, amount=%s, parts=%d)",
					i, v, currency, amount, parts)
			}
		}
	})
}

// TestPropProrateFee_CorrectLength asserts len(result) == parts.
func TestPropProrateFee_CorrectLength(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")
		parts := genParts().Draw(t, "parts")

		result, err := ProrateFee(amount, currency, parts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != parts {
			t.Fatalf("len=%d != parts=%d", len(result), parts)
		}
	})
}

// ---------------------------------------------------------------------------
// TaxSplit property tests
// ---------------------------------------------------------------------------

// TestPropTaxSplit_SumEqualsInput asserts that tax + net always equals the
// original amount (no money is created or destroyed by the split).
func TestPropTaxSplit_SumEqualsInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")
		rate := genTaxRate().Draw(t, "rate")

		tax, net, err := TaxSplit(amount, currency, rate)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !tax.Add(net).Equal(amount) {
			saveCounterexample(t, t.Name(), map[string]any{
				"currency": currency, "amount": amount.String(),
				"rate": rate.String(), "tax": tax.String(), "net": net.String(),
			})
			t.Fatalf("tax+net=%s != amount=%s (currency=%s, rate=%s)",
				tax.Add(net), amount, currency, rate)
		}
	})
}

// TestPropTaxSplit_NoNegativeComponents asserts both tax and net are >= 0.
func TestPropTaxSplit_NoNegativeComponents(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")
		rate := genTaxRate().Draw(t, "rate")

		tax, net, err := TaxSplit(amount, currency, rate)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tax.IsNegative() {
			saveCounterexample(t, t.Name(), map[string]any{
				"field": "tax", "currency": currency,
				"amount": amount.String(), "rate": rate.String(), "value": tax.String(),
			})
			t.Fatalf("tax=%s is negative", tax)
		}
		if net.IsNegative() {
			saveCounterexample(t, t.Name(), map[string]any{
				"field": "net", "currency": currency,
				"amount": amount.String(), "rate": rate.String(), "value": net.String(),
			})
			t.Fatalf("net=%s is negative", net)
		}
	})
}

// TestPropTaxSplit_ZeroRateIsIdentity asserts that a 0% tax rate yields
// tax==0 and net==amount.
func TestPropTaxSplit_ZeroRateIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")

		tax, net, err := TaxSplit(amount, currency, decimal.Zero)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !tax.IsZero() {
			t.Fatalf("zero-rate tax should be 0, got %s", tax)
		}
		if !net.Equal(amount) {
			t.Fatalf("zero-rate net should equal amount %s, got %s", amount, net)
		}
	})
}

// TestPropTaxSplit_FullRateIsAllTax asserts that a 100% tax rate yields
// net==0 and tax==RoundMoney(amount, currency).
func TestPropTaxSplit_FullRateIsAllTax(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")

		tax, net, err := TaxSplit(amount, currency, decimal.NewFromInt(1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := RoundMoney(amount, currency)
		if !tax.Equal(want) {
			t.Fatalf("full-rate tax should be %s, got %s", want, tax)
		}
		// net = amount - tax; since amount is already rounded, net must be 0
		if !net.IsZero() {
			t.Fatalf("full-rate net should be 0, got %s", net)
		}
	})
}

// ---------------------------------------------------------------------------
// RoundMoney property tests
// ---------------------------------------------------------------------------

// TestPropRoundMoney_Idempotent asserts that rounding an already-rounded
// value produces the same value (rounding is idempotent).
func TestPropRoundMoney_Idempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")

		once := RoundMoney(amount, currency)
		twice := RoundMoney(once, currency)

		if !once.Equal(twice) {
			t.Fatalf("RoundMoney not idempotent: once=%s twice=%s (currency=%s)",
				once, twice, currency)
		}
	})
}

// TestPropRoundMoney_ScaleRespected asserts the result has at most
// scaleForCurrency(currency) decimal places.
func TestPropRoundMoney_ScaleRespected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		currency := genCurrency().Draw(t, "currency")
		amount := genPositiveAmount(currency).Draw(t, "amount")

		result := RoundMoney(amount, currency)
		scale := scaleForCurrency(currency)

		// Shift left by scale; the result should be an integer
		shifted := result.Shift(scale)
		if !shifted.Equal(shifted.Floor()) {
			saveCounterexample(t, t.Name(), map[string]any{
				"currency": currency, "amount": amount.String(),
				"result": result.String(), "scale": scale,
			})
			t.Fatalf("RoundMoney result %s has more than %d decimal places (currency=%s)",
				result, scale, currency)
		}
	})
}

// ---------------------------------------------------------------------------
// Error-input tests (not property-driven; fixed boundary values)
// ---------------------------------------------------------------------------

// TestProrateFee_InvalidInputs verifies error returns for invalid arguments.
func TestProrateFee_InvalidInputs(t *testing.T) {
	t.Run("negative amount", func(t *testing.T) {
		_, err := ProrateFee(decimal.NewFromFloat(-1), "USD", 2)
		if err != ErrInvalidAmount {
			t.Fatalf("expected ErrInvalidAmount, got %v", err)
		}
	})
	t.Run("zero parts", func(t *testing.T) {
		_, err := ProrateFee(decimal.NewFromFloat(10), "USD", 0)
		if err != ErrInvalidParts {
			t.Fatalf("expected ErrInvalidParts, got %v", err)
		}
	})
	t.Run("negative parts", func(t *testing.T) {
		_, err := ProrateFee(decimal.NewFromFloat(10), "USD", -1)
		if err != ErrInvalidParts {
			t.Fatalf("expected ErrInvalidParts, got %v", err)
		}
	})
}

// TestTaxSplit_InvalidInputs verifies error returns for invalid arguments.
func TestTaxSplit_InvalidInputs(t *testing.T) {
	t.Run("negative amount", func(t *testing.T) {
		_, _, err := TaxSplit(decimal.NewFromFloat(-5), "USD", decimal.NewFromFloat(0.1))
		if err != ErrInvalidAmount {
			t.Fatalf("expected ErrInvalidAmount, got %v", err)
		}
	})
	t.Run("rate above 1", func(t *testing.T) {
		_, _, err := TaxSplit(decimal.NewFromFloat(100), "USD", decimal.NewFromFloat(1.01))
		if err != ErrInvalidTaxRate {
			t.Fatalf("expected ErrInvalidTaxRate, got %v", err)
		}
	})
	t.Run("negative rate", func(t *testing.T) {
		_, _, err := TaxSplit(decimal.NewFromFloat(100), "USD", decimal.NewFromFloat(-0.01))
		if err != ErrInvalidTaxRate {
			t.Fatalf("expected ErrInvalidTaxRate, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Currency edge-case deterministic tests
// ---------------------------------------------------------------------------

// TestProrateFee_JPY exercises a zero-decimal currency (JPY) where there
// must be no fractional sub-units in the output.
func TestProrateFee_JPY(t *testing.T) {
	// 1000 JPY split 3 ways → [334, 333, 333] (sum = 1000)
	parts, err := ProrateFee(decimal.NewFromInt(1000), "JPY", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := decimal.Zero
	for _, p := range parts {
		if !p.Equal(p.Floor()) {
			t.Fatalf("JPY part %s is fractional", p)
		}
		sum = sum.Add(p)
	}
	if !sum.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("JPY sum %s != 1000", sum)
	}
}

// TestProrateFee_BHD exercises a 3-decimal currency (BHD).
func TestProrateFee_BHD(t *testing.T) {
	// 10.000 BHD split 3 ways → each part should sum to exactly 10.000
	amount, _ := decimal.NewFromString("10.000")
	parts, err := ProrateFee(amount, "BHD", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := decimal.Zero
	for _, p := range parts {
		sum = sum.Add(p)
	}
	if !sum.Equal(amount) {
		t.Fatalf("BHD sum %s != %s", sum, amount)
	}
}

// TestTaxSplit_JPY verifies that tax and net are whole numbers for JPY.
func TestTaxSplit_JPY(t *testing.T) {
	// 1000 JPY at 10% tax → tax=100 JPY, net=900 JPY
	amount := decimal.NewFromInt(1000)
	rate, _ := decimal.NewFromString("0.10")
	tax, net, err := TaxSplit(amount, "JPY", rate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tax.Equal(tax.Floor()) {
		t.Fatalf("JPY tax %s is fractional", tax)
	}
	if !net.Equal(net.Floor()) {
		t.Fatalf("JPY net %s is fractional", net)
	}
	if !tax.Add(net).Equal(amount) {
		t.Fatalf("JPY tax+net=%s != %s", tax.Add(net), amount)
	}
}

// TestTaxSplit_BHD verifies a 3-decimal currency split.
func TestTaxSplit_BHD(t *testing.T) {
	amount, _ := decimal.NewFromString("99.999")
	rate, _ := decimal.NewFromString("0.05")
	tax, net, err := TaxSplit(amount, "BHD", rate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tax.Add(net).Equal(amount) {
		t.Fatalf("BHD tax+net=%s != %s", tax.Add(net), amount)
	}
	// tax must be rounded to 3 decimal places
	scale := scaleForCurrency("BHD")
	shifted := tax.Shift(scale)
	if !shifted.Equal(shifted.Floor()) {
		t.Fatalf("BHD tax %s exceeds 3 decimal places", tax)
	}
}
