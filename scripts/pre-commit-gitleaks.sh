#!/usr/bin/env bash
# =============================================================================
# pre-commit — gitleaks secret scan
#
# Installed by: make gitleaks-install-hooks  (or  scripts/install-gitleaks-hook.sh)
# Scans staged changes for secrets before every commit.
#
# REQUIREMENTS:
#   - gitleaks  (https://github.com/gitleaks/gitleaks)
#     Install via:  brew install gitleaks           # macOS
#                   scoop install gitleaks           # Windows
#                   curl -sSfL https://github.com/gitleaks/gitleaks/releases/latest/download/gitleaks-linux-amd64 -o /usr/local/bin/gitleaks && chmod +x /usr/local/bin/gitleaks
#
# SKIP:  SKIP=gitleaks git commit -m "msg"
# =============================================================================

set -euo pipefail

# Allow user to bypass with SKIP=gitleaks
if [ "${SKIP:-}" = "gitleaks" ]; then
  echo "gitleaks: skipping (SKIP=gitleaks)"
  exit 0
fi

# Check that gitleaks is installed
if ! command -v gitleaks &>/dev/null; then
  echo "gitleaks: not found — install from https://github.com/gitleaks/gitleaks"
  echo "  brew install gitleaks  |  scoop install gitleaks  |  see README"
  exit 1
fi

GITLEAKS_CONFIG="$(git rev-parse --show-toplevel)/.gitleaks.toml"

if [ ! -f "$GITLEAKS_CONFIG" ]; then
  echo "gitleaks: config not found at $GITLEAKS_CONFIG"
  exit 1
fi

echo "gitleaks: scanning staged changes..."

# Use protect --staged to scan only the diff that would be committed.
# This is fast even on large repos because gitleaks only examines changed lines.
gitleaks protect --source "$(git rev-parse --show-toplevel)" \
                 --config "$GITLEAKS_CONFIG" \
                 --staged \
                 --verbose \
                 --no-banner 2>/dev/null

exit_code=$?

if [ $exit_code -eq 0 ]; then
  echo "gitleaks: ✅ no secrets detected"
elif [ $exit_code -eq 1 ]; then
  echo ""
  echo "gitleaks: ❌ secrets or potential secrets were found in the staged diff."
  echo "  If the findings are false positives:"
  echo "    1. Add the path/regex to .gitleaks.toml under [allowlist]"
  echo "    2. Include a comment explaining why it is safe"
  echo "    3. Stage the updated .gitleaks.toml and retry"
  echo ""
  echo "  To bypass this check (not recommended): SKIP=gitleaks git commit ..."
  exit 1
fi

exit $exit_code