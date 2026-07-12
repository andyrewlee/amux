//go:build !windows

package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"
	"unicode"

	"github.com/andyrewlee/amux/internal/app"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/pprofhttp"
)

// ansiCSI matches CSI escape sequences so we can count actually-visible glyphs
// rather than escape codes when checking a frame is non-degenerate.
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// visibleRuneCount returns the number of non-whitespace, graphic runes left
// after stripping ANSI control sequences — a proxy for "the frame actually
// rendered content" rather than blank space or pure escape codes.
func visibleRuneCount(s string) int {
	clean := ansiCSI.ReplaceAllString(s, "")
	n := 0
	for _, r := range clean {
		if unicode.IsGraphic(r) && !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

type stats struct {
	avg time.Duration
	min time.Duration
	max time.Duration
	p50 time.Duration
	p95 time.Duration
	p99 time.Duration
}

func main() {
	perf.Init()
	startPprof()

	mode := flag.String("mode", app.HarnessCenter, "render mode: center, sidebar, or monitor")
	tabs := flag.Int("tabs", 16, "number of tabs/agents")
	width := flag.Int("width", 160, "screen width in columns")
	height := flag.Int("height", 48, "screen height in rows")
	frames := flag.Int("frames", 300, "number of measured frames")
	warmup := flag.Int("warmup", 30, "warmup frames to ignore")
	hotTabs := flag.Int("hot-tabs", 1, "number of tabs receiving animated output")
	payloadBytes := flag.Int("payload-bytes", 64, "bytes written per hot tab per frame")
	newlineEvery := flag.Int("newline-every", 0, "emit newline every N frames (0 disables)")
	showKeymapHints := flag.Bool("keymap-hints", false, "render keymap hints")
	overlay := flag.String("overlay", "", "render an overlay over the base pane: dialog, settings, prefix, error, or input (empty renders base pane only)")
	minVisible := flag.Int("assert-min-visible", 0, "fail (exit 1) if the final rendered frame has fewer than this many visible glyphs; 0 disables. Guards against renders that produce empty/garbage frames without crashing.")
	dumpFrame := flag.String("dump-frame", "", "write the final rendered view (full ANSI) to this path; empty disables. Lets callers diff/golden the exact frame an agent sees.")
	flag.Parse()

	opts := app.HarnessOptions{
		Mode:            *mode,
		Tabs:            *tabs,
		Width:           *width,
		Height:          *height,
		HotTabs:         *hotTabs,
		PayloadBytes:    *payloadBytes,
		NewlineEvery:    *newlineEvery,
		ShowKeymapHints: *showKeymapHints,
		Overlay:         *overlay,
	}

	h, err := app.NewHarness(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}

	totalFrames := *warmup + *frames
	if totalFrames <= 0 {
		fmt.Fprintln(os.Stderr, "frames + warmup must be > 0")
		os.Exit(1)
	}

	durations := make([]time.Duration, 0, *frames)
	startAll := time.Now()

	var lastVisible int
	var lastContent string
	for i := 0; i < totalFrames; i++ {
		h.Step(i)
		start := time.Now()
		view := h.Render()
		if i >= *warmup {
			durations = append(durations, time.Since(start))
			lastVisible = visibleRuneCount(view.Content)
			lastContent = view.Content
		}
	}

	if *dumpFrame != "" {
		if err := writeDumpFrame(*dumpFrame, lastContent); err != nil {
			fmt.Fprintf(os.Stderr, "harness: dump-frame write failed: %v\n", err)
			os.Exit(1)
		}
	}

	if *minVisible > 0 && lastVisible < *minVisible {
		fmt.Fprintf(os.Stderr, "harness: final frame has %d visible glyphs, want >= %d (render produced an empty/degenerate frame)\n",
			lastVisible, *minVisible)
		os.Exit(1)
	}

	total := time.Since(startAll)
	s := summarize(durations)
	fmt.Printf("mode=%s tabs=%d frames=%d warmup=%d size=%dx%d hot_tabs=%d payload=%dB newline_every=%d\n",
		*mode, *tabs, *frames, *warmup, *width, *height, *hotTabs, *payloadBytes, *newlineEvery)
	fmt.Printf("total=%s avg=%s p50=%s p95=%s p99=%s min=%s max=%s fps=%.2f\n",
		total, s.avg, s.p50, s.p95, s.p99, s.min, s.max, fps(durations))
	perf.Flush("harness")
}

func writeDumpFrame(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func summarize(durations []time.Duration) stats {
	if len(durations) == 0 {
		return stats{}
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return stats{
		avg: total / time.Duration(len(durations)),
		min: sorted[0],
		max: sorted[len(sorted)-1],
		p50: percentile(sorted, 0.50),
		p95: percentile(sorted, 0.95),
		p99: percentile(sorted, 0.99),
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := int(float64(len(sorted)-1) * p)
	if pos < 0 {
		pos = 0
	}
	if pos >= len(sorted) {
		pos = len(sorted) - 1
	}
	return sorted[pos]
}

func fps(durations []time.Duration) float64 {
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	if total <= 0 {
		return 0
	}
	return float64(len(durations)) / total.Seconds()
}

func startPprof() {
	addr, ok := pprofhttp.AddrFromEnvValue(os.Getenv("AMUX_PPROF"))
	if !ok {
		return
	}
	server := pprofhttp.NewServer(addr)

	go func() {
		fmt.Fprintf(os.Stderr, "pprof listening on %s\n", addr)
		if err := server.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "pprof server stopped: %v\n", err)
		}
	}()
}
