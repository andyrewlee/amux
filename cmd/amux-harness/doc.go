// Command amux-harness is a headless render/perf harness for amux. It drives the
// real UI render path without a TTY, producing deterministic per-frame timings
// for CI regression checks and local profiling.
//
// Modes (-mode): center, sidebar, or monitor — each exercises a different pane's
// render path.
//
// Useful flags: -tabs, -hot-tabs (tabs receiving animated output), -payload-bytes
// (bytes written per hot tab per frame), -newline-every, -frames (measured
// frames), -warmup (warmup frames to ignore), -width, -height, -keymap-hints.
//
// Set AMUX_PPROF=<path> to write a CPU profile of the run. The Makefile
// `harness-presets` target runs the center/sidebar/monitor presets that CI also
// runs; reproduce a CI harness failure by running the matching preset locally.
package main
