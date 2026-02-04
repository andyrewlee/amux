# Performance Baselines

Date: 2026-02-04

Machine
- Host: Andrews-MacBook-Pro
- OS: macOS (Darwin 24.6.0, arm64)
- CPU: Apple M1 Max (10 cores)
- Memory: 32 GB
- Go: go1.25.6

Harness Presets (terminal size 160x48)

Center (16 tabs, 2 hot tabs, 64B payload)
- Command: `go run ./cmd/amux-harness -mode center -tabs 16 -hot-tabs 2 -payload-bytes 64 -frames 300 -warmup 30 -width 160 -height 48`
- Result: `total=345.290042ms avg=1.038829ms p50=987.25µs p95=1.319417ms p99=1.943333ms min=906.125µs max=3.556ms fps=962.62`

Monitor (16 tabs, 4 hot tabs, 64B payload)
- Command: `go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames 300 -warmup 30 -width 160 -height 48`
- Result: `total=452.965791ms avg=1.346043ms p50=1.277416ms p95=1.744709ms p99=2.143916ms min=1.170375ms max=5.612041ms fps=742.92`

Sidebar (deep scrollback: newline every frame)
- Command: `go run ./cmd/amux-harness -mode sidebar -tabs 16 -hot-tabs 1 -payload-bytes 64 -newline-every 1 -frames 600 -warmup 30 -width 160 -height 48`
- Result: `total=844.516834ms avg=1.329752ms p50=1.266459ms p95=1.68ms p99=2.266375ms min=1.160375ms max=3.673417ms fps=752.02`

pprof Capture (AMUX_PPROF)
- Scenario: monitor preset under sustained load.
- Command: `AMUX_PPROF=6060 go run ./cmd/amux-harness -mode monitor -tabs 16 -hot-tabs 4 -payload-bytes 64 -frames 8000 -warmup 30 -width 160 -height 48`
- Profiles:
  - `perf/pprof/monitor_cpu.pprof`
  - `perf/pprof/monitor_heap.pprof`
