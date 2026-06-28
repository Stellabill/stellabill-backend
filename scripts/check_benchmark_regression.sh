#!/usr/bin/env bash
# scripts/check_benchmark_regression.sh
#
# Parse the output of `benchstat <baseline> <head>` and exit non-zero if any
# tracked benchmark regressed by more than THRESHOLD percent.
#
# Usage:
#   benchstat baseline.txt head.txt | ./scripts/check_benchmark_regression.sh [threshold]
#
# Arguments:
#   threshold   – regression ceiling as an integer percentage (default: 10)
#
# Exit codes:
#   0  – no regressions above the threshold (or no benchstat output to check)
#   1  – one or more benchmarks regressed beyond the threshold
#   2  – bad usage (wrong number of arguments, non-numeric threshold)

set -euo pipefail

THRESHOLD="${1:-10}"

# Validate threshold is a positive integer
if ! [[ "$THRESHOLD" =~ ^[0-9]+$ ]]; then
  echo "ERROR: threshold must be a non-negative integer, got: '$THRESHOLD'" >&2
  exit 2
fi

FAILED=0
CHECKED=0

while IFS= read -r line; do
  # Skip blank lines and benchstat header rows
  [[ -z "$line" ]]                        && continue
  [[ "$line" =~ ^(name|goos|goarch|pkg|cpu|PASS|ok|---|\s*$) ]] && continue

  # Extract the rightmost POSITIVE percentage token, e.g. "+13.64%".
  # Negative tokens (improvements) are intentionally ignored.
  delta=$(echo "$line" | grep -oE '\+[0-9]+\.[0-9]+%' | tail -1 || true)
  [[ -z "$delta" ]] && continue

  CHECKED=$((CHECKED + 1))

  # Strip '+' and '%' to obtain the magnitude
  magnitude=$(echo "$delta" | tr -d '+' | tr -d '%')

  # floating-point compare via awk
  is_regression=$(awk -v mag="$magnitude" -v thr="$THRESHOLD" \
    'BEGIN { print (mag + 0 > thr + 0) ? "yes" : "no" }')

  if [[ "$is_regression" == "yes" ]]; then
    echo "❌  REGRESSION (+${magnitude}% > ${THRESHOLD}%): $line"
    FAILED=$((FAILED + 1))
  fi
done

echo ""
echo "Checked ${CHECKED} benchmark delta(s)."

if [[ $FAILED -gt 0 ]]; then
  echo "❌  ${FAILED} regression(s) exceed the ${THRESHOLD}% threshold."
  exit 1
fi

echo "✅  All benchmarks within the ${THRESHOLD}% regression threshold."
exit 0