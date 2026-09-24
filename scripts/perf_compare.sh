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
  # µs is matched via a byte-safe pattern, not a literal: BSD awk on macOS
  # miscompares multibyte string literals ("s" == "µs" comes out true there).
  measured_ms=$(awk -v s="$p95" 'BEGIN {
    if (!match(s, /[0-9.]+/)) {
      print "unrecognized duration: " s > "/dev/stderr"; exit 1
    }
    v = substr(s, RSTART, RLENGTH); u = substr(s, RSTART + RLENGTH)
    if (u == "ns") print v * 1e-6;
    else if (u == "us" || u ~ /^[^a-zA-Z]+s$/) print v * 1e-3;
    else if (u == "ms") print v;
    else if (u == "s") print v * 1000;
    else { print "unrecognized duration: " s > "/dev/stderr"; exit 1 }
  }')

  local baseline_var="${prefix}_${name}_P95_MS"
  local baseline="${!baseline_var:-}"
  if [[ -z "$baseline" ]]; then
    if [[ "${PERF_STRICT:-0}" == "1" ]]; then
      echo "No baseline set for ${baseline_var}; PERF_STRICT=1 requires one (measured=${measured_ms}ms)." >&2
      failures=$((failures + 1))
    else
      echo "No baseline set for ${baseline_var}; skipping comparison (set PERF_STRICT=1 to fail instead)."
    fi
    return
  fi

  local threshold
  threshold=$(awk -v b="$baseline" -v t="$TOLERANCE" 'BEGIN { print b * (1.0 + t) }')

  echo "${name} p95: measured=${measured_ms}ms baseline=${baseline}ms threshold=${threshold}ms"

  local exceeds
  exceeds=$(awk -v m="$measured_ms" -v t="$threshold" 'BEGIN { print (m > t) ? 1 : 0 }')
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
