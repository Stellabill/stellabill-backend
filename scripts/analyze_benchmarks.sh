#!/bin/bash
# analyze_benchmarks.sh - Analyze benchmark results and detect regressions

set -e

if [ $# -lt 2 ]; then
    echo "Usage: $0 <baseline.txt> <new.txt> [threshold]"
    echo "Example: $0 baseline.txt new.txt 1.20"
    exit 1
fi

BASELINE=$1
NEW=$2
THRESHOLD=${3:-1.20}  # Default 20% regression threshold

if [ ! -f "$BASELINE" ]; then
    echo "Error: Baseline file not found: $BASELINE"
    exit 1
fi

if [ ! -f "$NEW" ]; then
    echo "Error: New benchmark file not found: $NEW"
    exit 1
fi

echo "Analyzing benchmarks..."
echo "Baseline: $BASELINE"
echo "New: $NEW"
echo "Regression threshold: ${THRESHOLD}x ($(echo "($THRESHOLD - 1) * 100" | bc)%)"
echo ""

# Check if benchstat is installed
if ! command -v benchstat &> /dev/null; then
    echo "Installing benchstat..."
    go install golang.org/x/perf/cmd/benchstat@latest
fi

# Run comparison
echo "=== Benchmark Comparison ==="
benchstat "$BASELINE" "$NEW" | tee comparison.txt

echo ""
echo "=== Regression Analysis ==="

# Parse results and check for regressions
REGRESSIONS=0

while IFS= read -r line; do
    # Look for lines with performance changes
    if echo "$line" | grep -qE "\+[0-9]+\.[0-9]+%"; then
        CHANGE=$(echo "$line" | grep -oE "\+[0-9]+\.[0-9]+" | head -1)
        PERCENT=$(echo "$CHANGE" | tr -d '+')
        
        # Convert to multiplier
        MULTIPLIER=$(echo "1 + $PERCENT / 100" | bc -l)
        
        # Check if exceeds threshold
        if (( $(echo "$MULTIPLIER > $THRESHOLD" | bc -l) )); then
            echo "⚠️  REGRESSION DETECTED: $line"
            REGRESSIONS=$((REGRESSIONS + 1))
        fi
    fi
done < comparison.txt

echo ""
if [ $REGRESSIONS -gt 0 ]; then
    echo "❌ Found $REGRESSIONS regression(s) exceeding ${THRESHOLD}x threshold"
    exit 1
else
    echo "✅ No significant regressions detected"
    exit 0
fi
