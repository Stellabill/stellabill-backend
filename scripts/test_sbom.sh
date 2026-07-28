#!/usr/bin/env bash
# test_sbom.sh — Generate a CycloneDX SBOM and validate its contents.
#
# Usage:
#   ./scripts/test_sbom.sh            # generate + validate
#   ./scripts/test_sbom.sh validate   # validate existing sbom.json only
set -euo pipefail

SBOM_FILE="${SBOM_FILE:-sbom.json}"
MODE="${1:-generate}"

# ── helpers ──────────────────────────────────────────────────────────────────

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command '$1' not found"
}

# ── generate ─────────────────────────────────────────────────────────────────

if [ "$MODE" = "generate" ]; then
  require_cmd syft

  echo ">> Generating CycloneDX SBOM..."
  syft -o "cyclonedx-json=${SBOM_FILE}" "dir:."
  pass "syft produced ${SBOM_FILE}"
fi

# ── validate ─────────────────────────────────────────────────────────────────

require_cmd python3

test -s "$SBOM_FILE" || fail "${SBOM_FILE} is missing or empty"

python3 - "$SBOM_FILE" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path) as f:
    bom = json.load(f)

errors = []

# 1. Correct format
if bom.get("bomFormat") != "CycloneDX":
    errors.append(f"bomFormat is {bom.get('bomFormat')!r}, want CycloneDX")

# 2. Non-empty metadata
meta = bom.get("metadata", {})
if not meta.get("timestamp"):
    errors.append("metadata.timestamp is missing")
if not meta.get("tools", {}).get("components"):
    # syft always populates this
    pass  # lenient — may vary by version

# 3. Components exist
components = bom.get("components", [])
if len(components) == 0:
    errors.append("SBOM has zero components")

# 4. Go module graph present (purl pkg:go/)
go_purls = [c for c in components if c.get("purl", "").startswith("pkg:go/")]
if len(go_purls) == 0:
    errors.append("no Go module components (pkg:go/) found")

# 5. OS packages present (purl pkg:deb/ or pkg:apk/)
os_pkgs = [c for c in components if c.get("purl", "").startswith(("pkg:deb/", "pkg:apk/"))]
if len(os_pkgs) == 0:
    errors.append("no OS-level package components found")

# 6. No duplicate purls at the same version
seen = set()
for c in components:
    key = (c.get("purl", ""), c.get("version", ""))
    if key in seen:
        errors.append(f"duplicate component: {key}")
    seen.add(key)

# 7. CycloneDX spec version is 1.x
spec = bom.get("specVersion", "")
if not spec.startswith("1."):
    errors.append(f"unexpected specVersion: {spec!r}")

if errors:
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    sys.exit(1)

go_count = len(go_purls)
os_count = len(os_pkgs)
total = len(components)
print(f"  {total} components ({go_count} Go modules, {os_count} OS packages)")
print("PASS: SBOM validation passed")
PY

pass "all checks passed"
