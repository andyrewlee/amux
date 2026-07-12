# Performance Baselines

Date: 2026-07-12

Machine
- Host: darwin-arm64 dev host (Apple Silicon)
- Go: go1.26.4

Enforced baselines (source of truth)

The enforced p95 baselines live in `scripts/perf_baselines.env`, keyed by
OS/ARCH (`DARWIN_ARM64_*` for the dev host, `LINUX_AMD64_*` for CI runners).
This document deliberately does not repeat the numbers so they cannot drift.

- Read the current values: `cat scripts/perf_baselines.env`
- Check against them: `make perf-check` (runs `scripts/perf_compare.sh`, which
  sources the `.env`, runs each harness preset below, parses the reported p95,
  and fails if it exceeds the baseline by more than `PERF_TOLERANCE`).
- Re-baseline: run `PERF_STRICT=1 make perf-check` three times on a quiescent
  host and commit the per-preset medians to `scripts/perf_baselines.env` (see
  the `perf-check` notes in the Makefile).

Harness Presets (terminal size 160x48)

Center (16 tabs, 2 hot tabs, 64B payload)
- Command: `go run ./cmd/amux-harness -mode center -tabs 16 -hot-tabs 2 -payload-bytes 64 -frames 300 -warmup 30 -width 160 -height 48`
- Baseline: `DARWIN_ARM64_CENTER_P95_MS` in `scripts/perf_baselines.env`

Monitor (16 tabs, 4 hot tabs, 64B payload)
- Command: `go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames 300 -warmup 30 -width 160 -height 48`
- Baseline: `DARWIN_ARM64_MONITOR_P95_MS` in `scripts/perf_baselines.env`

Sidebar (deep scrollback: newline every frame)
- Command: `go run ./cmd/amux-harness -mode sidebar -tabs 16 -hot-tabs 1 -payload-bytes 64 -newline-every 1 -frames 600 -warmup 30 -width 160 -height 48`
- Baseline: `DARWIN_ARM64_SIDEBAR_P95_MS` in `scripts/perf_baselines.env`

pprof Capture (AMUX_PPROF)
- Scenario: monitor preset under sustained load.
- Command: `AMUX_PPROF=6060 go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames 8000 -warmup 30 -width 160 -height 48`
- Profiles:
  - `perf/pprof/monitor_cpu.pprof`
  - `perf/pprof/monitor_heap.pprof`
