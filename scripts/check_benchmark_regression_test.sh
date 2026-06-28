#!/usr/bin/env bash
# scripts/check_benchmark_regression_test.sh
#
# Unit tests for check_benchmark_regression.sh.
#
# Run from the repository root:
#   bash scripts/check_benchmark_regression_test.sh
#
# Exit code 0 means all tests passed.

set -euo pipefail

SCRIPT="$(dirname "$0")/check_benchmark_regression.sh"
PASS=0
FAIL=0

# ---------------------------------------------------------------------------
# Helper: run one test case
#   assert_exit <expected_exit_code> <test_name> <<< "stdin"
# ---------------------------------------------------------------------------
assert_exit() {
  local expected="$1"
  local name="$2"
  # stdin is already set up by caller via here-doc redirect

  actual_exit=0
  bash "$SCRIPT" 10 || actual_exit=$?

  if [[ "$actual_exit" -eq "$expected" ]]; then
    echo "  PASS  $name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $name  (expected exit $expected, got $actual_exit)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== check_benchmark_regression.sh unit tests ==="
echo ""

# ---------------------------------------------------------------------------
# 1. No regressions – output has only improvements / stable lines
# ---------------------------------------------------------------------------
assert_exit 0 "no regressions (improvements only)" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Small-4           5.00µs ± 1%    4.80µs ± 2%   -4.00% (p=0.001 n=10)
BenchmarkListPlans_Medium-4         15.00µs ± 2%   14.50µs ± 1%   -3.33% (p=0.001 n=10)
BenchmarkListSubscriptions_Small-4   6.00µs ± 1%    6.10µs ± 2%   +1.67% (p=0.250 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# 2. One benchmark regresses exactly at the threshold (10 %) → should PASS
#    (threshold is strictly greater-than, so 10.00 % is fine)
# ---------------------------------------------------------------------------
assert_exit 0 "regression exactly at threshold (10.00%) is allowed" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Small-4           5.00µs ± 1%    5.50µs ± 1%  +10.00% (p=0.001 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# 3. One benchmark regresses just above the threshold → should FAIL
# ---------------------------------------------------------------------------
assert_exit 1 "regression just above threshold (10.01%) is rejected" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Small-4           5.00µs ± 1%    5.51µs ± 1%  +10.01% (p=0.001 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# 4. Multiple regressions – at least one above threshold → FAIL
# ---------------------------------------------------------------------------
assert_exit 1 "multiple benchmarks – one regresses beyond threshold" << 'BENCHSTAT'
name                                   old time/op    new time/op    delta
BenchmarkListPlans_Small-4               5.00µs ± 1%    4.90µs ± 2%   -2.00% (p=0.020 n=10)
BenchmarkListSubscriptions_Medium-4     15.00µs ± 2%   17.00µs ± 2%  +13.33% (p=0.000 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# 5. Blank / header-only input (first run, no benchmarks match) → PASS
#    (nothing to regress against)
# ---------------------------------------------------------------------------
assert_exit 0 "empty / header-only input passes" << 'BENCHSTAT'
goos: linux
goarch: amd64
pkg: stellarbill-backend/internal/handlers

BENCHSTAT

# ---------------------------------------------------------------------------
# 6. New benchmark that did not exist in the baseline – benchstat emits it
#    without a delta column.  The script must not crash and should PASS.
# ---------------------------------------------------------------------------
assert_exit 0 "new benchmark with no delta column does not crash" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Small-4           5.00µs ± 1%    5.00µs ± 1%    ~     (p=0.800 n=10)
BenchmarkListPlans_NewBench-4              (new)    3.00µs ± 1%
BENCHSTAT

# ---------------------------------------------------------------------------
# 7. Invalid (non-numeric) threshold argument → exit 2
# ---------------------------------------------------------------------------
actual_exit=0
bash "$SCRIPT" "notanumber" <<< "" || actual_exit=$?
if [[ "$actual_exit" -eq 2 ]]; then
  echo "  PASS  invalid threshold exits with code 2"
  PASS=$((PASS + 1))
else
  echo "  FAIL  invalid threshold (expected exit 2, got $actual_exit)"
  FAIL=$((FAIL + 1))
fi

# ---------------------------------------------------------------------------
# 8. Very large regression (>100 %) → FAIL
# ---------------------------------------------------------------------------
assert_exit 1 "extreme regression (>100%) is rejected" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Large-4           5.00µs ± 1%   15.00µs ± 1%  +200.00% (p=0.000 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# 9. Negative regression (improvement) that looks like a large number
#    should never trip the gate.
# ---------------------------------------------------------------------------
assert_exit 0 "large improvement (-50%) does not trigger gate" << 'BENCHSTAT'
name                               old time/op    new time/op    delta
BenchmarkListPlans_Medium-4         10.00µs ± 1%    5.00µs ± 1%  -50.00% (p=0.000 n=10)
BENCHSTAT

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[[ $FAIL -eq 0 ]]