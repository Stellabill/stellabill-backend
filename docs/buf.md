# Buf — OpenAPI & Proto Linting

This document describes how [Buf](https://buf.build) is used in this repository to enforce spec quality and catch breaking changes before they reach production.

---

## Why Buf?

| Concern | Without Buf | With Buf |
|---|---|---|
| Proto naming conventions | Manual review | Enforced by `buf lint` |
| Breaking API changes | Discovered post-deploy | Blocked at PR time |
| OpenAPI spec quality | `go run ./cmd/openapi-validate` only | Additional lint via buf plugins |
| Code generation consistency | Ad-hoc `protoc` invocations | Reproducible `buf generate` |

---

## Configuration files

| File | Purpose |
|---|---|
| `buf.yaml` | Module definition, lint rules, breaking-change rules |
| `buf.gen.yaml` | Code-generation plugin skeleton (inactive until protos are added) |
| `proto/` | Future `.proto` service definitions |
| `scripts/buf-lint.sh` | Local lint + breaking-change wrapper |
| `.github/workflows/buf.yml` | CI workflow |

---

## Installation

```bash
# macOS
brew install bufbuild/buf/buf

# Linux (binary)
curl -sSL \
  "https://github.com/bufbuild/buf/releases/download/v1.32.2/buf-Linux-x86_64" \
  -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Verify
buf --version
```

---

## Running locally

```bash
# Lint only (checks naming, style, unused imports)
buf lint

# Lint + breaking-change detection against main
./scripts/buf-lint.sh --breaking

# Explicit breaking check against a specific branch
buf breaking --against '.git#branch=main'

# Generate stubs (after adding .proto files)
buf generate
```

---

## Lint rules

The active rule set is `DEFAULT` with one relaxation:

- **`FIELD_LOWER_SNAKE_CASE` disabled** — OpenAPI-generated stubs use camelCase field names on the wire. This rule would fire false positives on generated code.

All other `DEFAULT` rules are enforced, including:

- `PACKAGE_LOWER_SNAKE_CASE`
- `SERVICE_SUFFIX` (services must end in `Service`)
- `RPC_REQUEST_RESPONSE_UNIQUE`
- `ENUM_ZERO_VALUE_SUFFIX` (`_UNSPECIFIED`)

---

## Breaking-change detection

Breaking changes are detected against the base branch on every pull request. The following changes **block a merge**:

- Removing a field or message
- Changing a field number or type
- Removing an enum value
- Renaming an RPC method

To intentionally introduce a breaking change, document it in the PR description and get explicit approval from a maintainer.

---

## Pre-commit hook (optional)

Add the following to `.git/hooks/pre-commit` to run lint before every commit:

```bash
#!/usr/bin/env bash
set -euo pipefail
if command -v buf &>/dev/null; then
  buf lint || { echo "buf lint failed — commit blocked"; exit 1; }
fi
```

---

## Adding proto files

1. Create `proto/<service>.proto` following [Buf style guide](https://buf.build/docs/best-practices/style-guide).
2. Run `buf lint` to verify naming.
3. Uncomment the generator plugins in `buf.gen.yaml`.
4. Run `buf generate` to produce Go stubs in `gen/go/`.
5. Update `go.mod` with any new generated-code dependencies.

---

## CI

The `buf.yml` workflow runs on every push and pull request that touches `proto/`, `openapi/`, or the buf config files. It is path-filtered to avoid running on unrelated changes.

Breaking-change detection only runs on pull requests (not on direct pushes to `main`) because it requires a clear base branch to compare against.
