# Mutation testing — subscription state machine

Mutation testing verifies that the test suite actually catches bugs.
It automatically introduces small, deliberate faults (mutants) into the
production code and checks whether at least one test fails.

## Tool

We use the [avito-tech/go-mutesting](https://github.com/avito-tech/go-mutesting)
fork. It mutates the source file in place (with automatic restore) and runs:

    go test ./internal/subscriptions/...

## Gate

Merges touching `internal/subscriptions/` require a **mutation score ≥ 0.80**
(killed / total mutants). The check runs in CI via `.github/workflows/mutation.yml`.

## Local reproduction

```bash
# Install the tool
go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest

# Run mutation tests
make mutation-state-machine

# Or directly
go-mutesting ./internal/subscriptions/...
```

### Understanding the output

| Label  | Meaning                        | Desired? |
|--------|--------------------------------|----------|
| PASS   | Mutant **killed**              | ✅       |
| FAIL   | Mutant **survived**            | ❌       |
| SKIP   | Mutation did not compile       | —        |

The final line reports the **mutation score** (killed / total). A score of
`1.000000` means every mutant was caught.

## Score interpretation

- `0.80` = 80 % of mutants killed.  This is the minimum gate.
- `1.00` = 100 % of mutants killed.  Target for safety-critical packages.

### Killing a surviving mutant

1. Run `go-mutesting ./internal/subscriptions/...`.
2. Find the `FAIL` entries — these are surviving mutants.
3. For each survivor, examine the printed diff to understand what changed.
4. Write a test that specifically asserts the invariant the mutation broke.
5. Re-run to confirm the mutant is now killed (PASS).

## Known quirks

- `go-mutesting` uses the **built-in exec** by default (no `--exec` needed).
  That exec copies the mutated file over the original, runs `go test`, and
  restores the original.
- If you pass a custom `--exec` you are responsible for making the mutated
  file available to the test runner.
- The tool adds a `break` statement and `statement/remove` mutations that may
  not compile — those are skipped automatically.
