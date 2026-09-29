# Renovate configuration

Renovate is configured in [`renovate.json`](../renovate.json) to keep dependencies
up to date with minimal noise while preventing regressions.

## How it works

| Concern | Decision |
|---------|----------|
| Schedule | Runs every weekend so batched PRs arrive on Monday and don't interrupt the week |
| Stability window | `stabilityDays: 3` — Renovate waits 3 days after a version is published before raising a PR, reducing exposure to yanked releases |
| Dependency Dashboard | Enabled — a single tracking issue lists all pending/blocked updates at a glance |

## Package rules

### Go modules — minor / patch
Auto-merged via a squash commit once the branch CI (`lint`, `test`, `coverage`)
passes and the stability window has elapsed. No human review needed.

### Go modules — major
A separate PR is opened per package (grouped under the `go-major` slug so
reviewers can see the full blast-radius together). The `needs-review` and
`major-upgrade` labels are applied; automerge is disabled. A reviewer must
approve and merge manually.

### OpenTelemetry packages
All `go.opentelemetry.io/*` dependencies are grouped into a single
**`opentelemetry`** PR. OTel packages are versioned and released in lock-step,
so splitting them into separate PRs would create transient incompatibilities.

### Go toolchain
The `go` directive in `go.mod` is treated separately from module dependencies.
It is never auto-merged — it requires explicit review because it changes the
minimum Go version required to build the project.

### GitHub Actions
Minor/patch action version bumps are auto-merged. Major bumps (e.g., `v3` →
`v4` for `actions/checkout`) require manual review.

## Enabling Renovate

1. Install the [Renovate GitHub App](https://github.com/apps/renovate) on the
   `Stellabill` organisation and grant it access to this repository.
2. Merge `renovate.json` into `main` — Renovate will open an onboarding PR
   automatically.
3. Optionally configure required status checks in the branch-protection settings
   so that `automerge: true` can only trigger after CI is green.

## Validation

The config was validated offline with:

```
npx renovate config-validator renovate.json   # exits 0, no errors
```

The validator is provided by the `renovate` npm package itself (v44.0.0 at time
of writing).
