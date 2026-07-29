#!/usr/bin/env bash
# scripts/buf-lint.sh
# Run Buf lint and breaking-change checks against main.
# Used by CI (see .github/workflows/buf.yml) and pre-commit hooks.
#
# Usage:
#   ./scripts/buf-lint.sh              # lint only
#   ./scripts/buf-lint.sh --breaking   # lint + breaking-change detection
#
# Requirements: buf must be installed (https://buf.build/docs/installation)
# In CI the workflow installs buf via bufbuild/buf-setup-action.

set -euo pipefail

BUF_VERSION_MIN="1.28.0"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[buf-lint] $*"; }
die()  { echo "[buf-lint] ERROR: $*" >&2; exit 1; }

require_buf() {
  if ! command -v buf &>/dev/null; then
    die "buf is not installed. See https://buf.build/docs/installation"
  fi

  local version
  version=$(buf --version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  log "buf version: ${version}"
}

# ---------------------------------------------------------------------------
# Lint
# ---------------------------------------------------------------------------
run_lint() {
  log "Running buf lint…"
  buf lint
  log "Lint passed ✓"
}

# ---------------------------------------------------------------------------
# Breaking-change detection
# ---------------------------------------------------------------------------
run_breaking() {
  local against="${BREAKING_AGAINST:-.git#branch=main}"
  log "Checking breaking changes against ${against}…"
  buf breaking --against "${against}"
  log "No breaking changes detected ✓"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
require_buf

BREAKING=false
for arg in "$@"; do
  [[ "$arg" == "--breaking" ]] && BREAKING=true
done

run_lint

if [[ "$BREAKING" == "true" ]]; then
  run_breaking
fi

log "All checks passed."
