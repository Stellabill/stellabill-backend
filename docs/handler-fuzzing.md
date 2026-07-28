# Handler fuzzing

The handler package now includes native Go fuzz tests for the public API parsing paths that are most exposed to untrusted input:

- FuzzListPlans exercises plan listing query parsing and cursor handling.
- FuzzListStatements exercises statement filter and pagination parsing.
- FuzzSwapInput exercises shared swap request parsing for both swap endpoints.

## Running locally

```bash
go test -run=^$ -fuzz=FuzzListPlans -fuzztime=10s ./internal/handlers/...
go test -run=^$ -fuzz=FuzzListStatements -fuzztime=10s ./internal/handlers/...
go test -run=^$ -fuzz=FuzzSwapInput -fuzztime=10s ./internal/handlers/...
```

## Seed corpus locations

The fuzz targets load persisted regression seeds from `internal/handlers/testdata/fuzz/`:

- `internal/handlers/testdata/fuzz/FuzzListPlans/`
- `internal/handlers/testdata/fuzz/FuzzListStatements/`
- `internal/handlers/testdata/fuzz/FuzzSwapInput/`
- `internal/handlers/testdata/fuzz/FuzzSwapExactIn/`
- `internal/handlers/testdata/fuzz/FuzzSwapExactOut/`
- `internal/handlers/testdata/fuzz/FuzzSwapRawBody/`

To add a new seed, create a small file under the target directory and commit it.
The corpus is intentionally small and readable so regression inputs stay easy to review.

The nightly workflow runs each target with `-fuzztime=60s`.
