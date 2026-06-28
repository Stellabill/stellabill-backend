#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  cat <<'EOF' >&2
usage: scripts/capacity-collect.sh <metrics-url> <window-seconds> [output-dir]

Captures a baseline and peak Prometheus snapshot for capacity planning.
Example:
  scripts/capacity-collect.sh http://localhost:8080/api/metrics 300 ./artifacts/capacity
EOF
  exit 2
fi

metrics_url="$1"
window_seconds="$2"
output_dir="${3:-./artifacts/capacity}"

mkdir -p "$output_dir"

base_ts="$(date -u +%Y%m%dT%H%M%SZ)"
curl -fsSL "$metrics_url" > "$output_dir/baseline-$base_ts.prom"
sleep "$window_seconds"
peak_ts="$(date -u +%Y%m%dT%H%M%SZ)"
curl -fsSL "$metrics_url" > "$output_dir/peak-$peak_ts.prom"

cat > "$output_dir/metadata-$base_ts.json" <<EOF
{
  "metrics_url": "$metrics_url",
  "window_seconds": $window_seconds,
  "baseline_snapshot": "baseline-$base_ts.prom",
  "peak_snapshot": "peak-$peak_ts.prom"
}
EOF

echo "Wrote baseline-$base_ts.prom and peak-$peak_ts.prom to $output_dir"
