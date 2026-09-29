# Fuzz Test Corpora

Persisted minimized fuzz corpora for regression coverage. These files are
committed to disk so past failures are always re-tested and the corpus
grows over time.

## Corpus Files

| File | Target | Coverage |
|------|--------|----------|
| `FuzzPlanParsing` | `internal/handlers/plans_fuzz_test.go` | Plan JSON parsing edge cases |
| `FuzzStatementParsing` | `internal/handlers/statements_fuzz_test.go` | Statement period/amount validation |
| `FuzzSwapCalculation` | `internal/handlers/swaps_fuzz_test.go` | Cross-currency swap computation |

## Edge Cases Covered
- Empty inputs
- Null/missing fields
- Negative amounts
- Unicode/special characters
- Extremely large values
- XSS injection attempts
- Invalid periods/dates
- Zero amounts

## Usage
```bash
go test -fuzz=FuzzPlanParsing -fuzzdir=testdata/fuzz ./internal/handlers/
go test -fuzz=FuzzStatementParsing -fuzzdir=testdata/fuzz ./internal/handlers/
```
