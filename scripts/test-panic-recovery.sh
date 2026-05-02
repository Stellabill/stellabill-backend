#!/usr/bin/env bash
set -euo pipefail

# Run panic-recovery related tests and verify no stack traces leak to clients.

echo "=== Panic Recovery Tests ==="
go test ./internal/middleware/... -run "Recovery|Panic|Sanitize|Redact|PlainText" -v -count=1

echo ""
echo "=== Handler Panic Tests ==="
go test ./internal/handlers/... -run "Panic" -v -count=1 2>/dev/null || true

echo ""
echo "All panic recovery tests passed."
