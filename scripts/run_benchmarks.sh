#!/bin/bash
# run_benchmarks.sh - Execute benchmark suite and generate reports

set -e

BENCHMARK_DIR="benchmark_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="${BENCHMARK_DIR}/benchmark_${TIMESTAMP}.txt"

# Create results directory
mkdir -p "$BENCHMARK_DIR"

echo "Running benchmark suite..."
echo "Results will be saved to: $OUTPUT_FILE"
echo ""

# Run benchmarks
go test ./internal/handlers/... \
    -bench=. \
    -benchmem \
    -benchtime=3s \
    -timeout=30m \
    | tee "$OUTPUT_FILE"

echo ""
echo "Benchmark complete!"
echo ""

# Generate summary
echo "=== Summary ===" | tee -a "$OUTPUT_FILE"
echo "" | tee -a "$OUTPUT_FILE"

# Extract key metrics
echo "Plans Endpoint:" | tee -a "$OUTPUT_FILE"
grep "BenchmarkListPlans_" "$OUTPUT_FILE" | grep -v "Parallel\|JSON\|HTTP" | head -5

echo "" | tee -a "$OUTPUT_FILE"
echo "Subscriptions Endpoint:" | tee -a "$OUTPUT_FILE"
grep "BenchmarkListSubscriptions_" "$OUTPUT_FILE" | grep -v "Parallel\|JSON\|HTTP" | head -5

echo ""
echo "Full results saved to: $OUTPUT_FILE"

# Compare with baseline if exists
BASELINE="${BENCHMARK_DIR}/baseline.txt"
if [ -f "$BASELINE" ]; then
    echo ""
    echo "Comparing with baseline..."
    
    if command -v benchstat &> /dev/null; then
        benchstat "$BASELINE" "$OUTPUT_FILE"
    else
        echo "Install benchstat for comparison: go install golang.org/x/perf/cmd/benchstat@latest"
    fi
fi

echo ""
echo "To set this as baseline: cp $OUTPUT_FILE $BASELINE"
