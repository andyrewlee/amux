#!/usr/bin/env bash
set -euo pipefail

BASELINE_FILE="${PERF_BASELINE_FILE:-scripts/perf_baselines.env}"
TOLERANCE="${PERF_TOLERANCE:-0.10}"

if [[ ! -f "$BASELINE_FILE" ]]; then
  echo "Baseline file not found: $BASELINE_FILE" >&2
  exit 1
fi

# shellcheck source=/dev/null
source "$BASELINE_FILE"

os=$(go env GOOS | tr '[:lower:]' '[:upper:]')
arch=$(go env GOARCH | tr '[:lower:]' '[:upper:]')
prefix="${os}_${arch}"

failures=0

run_preset() {
  local name="$1"
  local target="$2"

  echo "Running $name preset..."
  local out
  out=$(make "$target")
  echo "$out"

  local p95
  p95=$(echo "$out" | tail -n 1 | sed -n 's/.*p95=\([^ ]*\).*/\1/p')
  if [[ -z "$p95" ]]; then
    echo "Failed to parse p95 for $name" >&2
    failures=$((failures + 1))
    return
  fi

  local measured_ms
  measured_ms=$(python3 - "$p95" <<'PY'
import sys
s = sys.argv[1]
units = [("ns", 1e-6), ("us", 1e-3), ("µs", 1e-3), ("ms", 1), ("s", 1000)]
for unit, factor in units:
    if s.endswith(unit):
        v = float(s[: -len(unit)])
        print(v * factor)
        break
else:
    raise SystemExit(f"unrecognized duration: {s}")
PY
)

  local baseline_var="${prefix}_${name}_P95_MS"
  local baseline="${!baseline_var:-}"
  if [[ -z "$baseline" ]]; then
    echo "No baseline set for ${baseline_var}; skipping comparison."
    return
  fi

  local threshold
  threshold=$(python3 - "$baseline" "$TOLERANCE" <<'PY'
import sys
baseline = float(sys.argv[1])
tol = float(sys.argv[2])
print(baseline * (1.0 + tol))
PY
)

  echo "${name} p95: measured=${measured_ms}ms baseline=${baseline}ms threshold=${threshold}ms"

  local exceeds
  exceeds=$(python3 - "$measured_ms" "$threshold" <<'PY'
import sys
measured = float(sys.argv[1])
threshold = float(sys.argv[2])
print("1" if measured > threshold else "0")
PY
)
  if [[ "$exceeds" == "1" ]]; then
    echo "${name} p95 exceeded threshold" >&2
    failures=$((failures + 1))
  fi
}

run_preset CENTER harness-center
run_preset SIDEBAR harness-sidebar
run_preset MONITOR harness-monitor

if [[ $failures -gt 0 ]]; then
  echo "Perf comparison failed (${failures} preset(s) over threshold)." >&2
  exit 1
fi

echo "Perf comparison passed."
