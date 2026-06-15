// Command amux-harness is a headless render/perf harness for amux. It drives the
// real UI render path without a TTY, producing deterministic per-frame timings
// for CI regression checks and local profiling.
//
// Modes (-mode): center, sidebar, or monitor — each exercises a different pane's
// render path.
//
// Useful flags: -tabs, -hot-tabs (tabs receiving animated output), -payload-bytes
// (bytes written per hot tab per frame), -newline-every, -frames (measured
// frames), -warmup (warmup frames to ignore), -width, -height, -keymap-hints,
// -dump-frame (write the final rendered view as raw ANSI bytes to a path — the
// exact frame an agent sees; `cat`/diff it to inspect, or feed it into a golden),
// -assert-min-visible (fail if the final frame has fewer than N visible glyphs).
//
// Set AMUX_PPROF=1/true, a port, or a listen address to start net/http/pprof
// (default 127.0.0.1:6060 for 1/true). Fetch CPU profiles from the pprof
// endpoint while the harness is running, for example /debug/pprof/profile.
// The Makefile `harness-presets` target runs heavier local confidence presets;
// CI uses shorter direct harness invocations.
package main
