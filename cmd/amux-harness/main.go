package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/andyrewlee/amux/internal/app"
)

type stats struct {
	avg time.Duration
	min time.Duration
	max time.Duration
	p50 time.Duration
	p95 time.Duration
	p99 time.Duration
}

func main() {
	mode := flag.String("mode", app.HarnessMonitor, "render mode: monitor, center, or sidebar")
	tabs := flag.Int("tabs", 16, "number of tabs/agents")
	width := flag.Int("width", 160, "screen width in columns")
	height := flag.Int("height", 48, "screen height in rows")
	frames := flag.Int("frames", 300, "number of measured frames")
	warmup := flag.Int("warmup", 30, "warmup frames to ignore")
	hotTabs := flag.Int("hot-tabs", 1, "number of tabs receiving animated output")
	payloadBytes := flag.Int("payload-bytes", 64, "bytes written per hot tab per frame")
	newlineEvery := flag.Int("newline-every", 0, "emit newline every N frames (0 disables)")
	showKeymapHints := flag.Bool("keymap-hints", false, "render keymap hints")
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

	for i := 0; i < totalFrames; i++ {
		h.Step(i)
		start := time.Now()
		view := h.Render()
		_ = view.Content
		if i >= *warmup {
			durations = append(durations, time.Since(start))
		}
	}

	total := time.Since(startAll)
	s := summarize(durations)
	fmt.Printf("mode=%s tabs=%d frames=%d warmup=%d size=%dx%d hot_tabs=%d payload=%dB newline_every=%d\n",
		*mode, *tabs, *frames, *warmup, *width, *height, *hotTabs, *payloadBytes, *newlineEvery)
	fmt.Printf("total=%s avg=%s p50=%s p95=%s p99=%s min=%s max=%s fps=%.2f\n",
		total, s.avg, s.p50, s.p95, s.p99, s.min, s.max, fps(durations))
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
