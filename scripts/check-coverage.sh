#!/usr/bin/env bash
# check-coverage.sh <coverage.out> <threshold>
# Fails if total coverage is below the threshold percentage.
set -euo pipefail

COVERAGE_FILE="${1:?usage: check-coverage.sh <coverage.out> <threshold>}"
THRESHOLD="${2:?usage: check-coverage.sh <coverage.out> <threshold>}"

TOTAL=$(go tool cover -func="$COVERAGE_FILE" | awk '/^total:/{gsub(/%/,"",$3); print $3}')

echo "Total coverage: ${TOTAL}%  (required: ${THRESHOLD}%)"

if awk "BEGIN{exit !($TOTAL < $THRESHOLD)}"; then
  echo "FAIL: coverage ${TOTAL}% is below the required ${THRESHOLD}%"
  exit 1
fi

echo "PASS: coverage threshold met."
