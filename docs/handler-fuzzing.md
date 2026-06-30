# Handler fuzzing

The handler package now includes native Go fuzz tests for the public API parsing paths that are most exposed to untrusted input:

- FuzzListPlans exercises plan listing query parsing and cursor handling.
- FuzzListStatements exercises statement filter and pagination parsing.
- FuzzSwapInput exercises swap request parsing for both swap endpoints.

## Running locally

```bash
go test -run=^$ -fuzz=FuzzListPlans -fuzztime=10s ./internal/handlers/...
go test -run=^$ -fuzz=FuzzListStatements -fuzztime=10s ./internal/handlers/...
go test -run=^$ -fuzz=FuzzSwapInput -fuzztime=10s ./internal/handlers/...
```

The nightly workflow runs each target with `-fuzztime=60s`.
