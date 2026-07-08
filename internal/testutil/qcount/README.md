# Handler query-count probe

`qcount` is a test-only `pgx.QueryTracer` used to detect N+1 query regressions.
It is attached while constructing an integration-test pool, so production
repositories and handlers remain unchanged.

For each handler request, call `Probe.Track` and put the returned context on the
HTTP request. Compare a small and large response with
`CheckResultSizeInvariant`. The larger response may execute only a configured
fixed number of additional queries. Use that allowance for legitimate
fixed-cost behavior such as validating an expanded filter; do not use it to
permit one query per result.

The live handler checks use the `integration` build tag because they start an
ephemeral PostgreSQL container:

```sh
go test -tags=integration ./internal/handlers/...
```

The probe is safe for parallel requests because each count is context-scoped.
Diagnostics include SQL text but deliberately exclude bound arguments, which
may contain tenant IDs, tokens, or customer data. Stored diagnostics are capped
at 256 statements and 4 KiB per statement to bound test memory and output.
