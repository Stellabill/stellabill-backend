#!/usr/bin/env bash
# =============================================================================
# install-gitleaks-hook.sh
#
# Installs the gitleaks pre-commit hook into .git/hooks/pre-commit.
# Safe to run multiple times — backs up any existing pre-commit hook first.
#
# Usage:
#   scripts/install-gitleaks-hook.sh
#
# Or via Make:
#   make gitleaks-install-hooks
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOK_SRC="$SCRIPT_DIR/pre-commit-gitleaks.sh"
HOOK_DST="$REPO_ROOT/.git/hooks/pre-commit"

if [ ! -d "$REPO_ROOT/.git" ]; then
  echo "error: not a git repository — $REPO_ROOT/.git not found"
  exit 1
fi

if [ ! -f "$HOOK_SRC" ]; then
  echo "error: source hook not found at $HOOK_SRC"
  exit 1
fi

# Backup existing hook if present
if [ -f "$HOOK_DST" ] && [ ! -L "$HOOK_DST" ]; then
  backup="${HOOK_DST}.bak.$(date +%Y%m%d%H%M%S)"
  echo "backing up existing hook to $backup"
  cp "$HOOK_DST" "$backup"
fi

install -m 0755 "$HOOK_SRC" "$HOOK_DST"
echo "gitleaks pre-commit hook installed at $HOOK_DST"
echo ""
echo "The hook will scan all staged changes for secrets before each commit."
echo "To bypass temporarily:  SKIP=gitleaks git commit ..."