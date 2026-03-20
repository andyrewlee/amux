//go:build !windows

package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/app"
	"github.com/andyrewlee/amux/internal/harnessbench"
	"github.com/andyrewlee/amux/internal/perf"
)

func main() {
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

	report, err := harnessbench.Run(harnessbench.Config{
		HarnessOptions: opts,
		Frames:         *frames,
		Warmup:         *warmup,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("mode=%s tabs=%d frames=%d warmup=%d size=%dx%d hot_tabs=%d payload=%dB newline_every=%d\n",
		*mode, *tabs, *frames, *warmup, *width, *height, *hotTabs, *payloadBytes, *newlineEvery)
	fmt.Printf("total=%s avg=%s p50=%s p95=%s p99=%s min=%s max=%s fps=%.2f\n",
		report.Total,
		report.Stats.Avg,
		report.Stats.P50,
		report.Stats.P95,
		report.Stats.P99,
		report.Stats.Min,
		report.Stats.Max,
		report.FPS,
	)
	perf.Flush("harness")
}

func startPprof() {
	raw := strings.TrimSpace(os.Getenv("AMUX_PPROF"))
	if raw == "" {
		return
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no":
		return
	}

	addr := raw
	if raw == "1" || strings.ToLower(raw) == "true" {
		addr = "127.0.0.1:6060"
	} else if _, err := strconv.Atoi(raw); err == nil {
		addr = "127.0.0.1:" + raw
	}

	go func() {
		fmt.Fprintf(os.Stderr, "pprof listening on %s\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Fprintf(os.Stderr, "pprof server stopped: %v\n", err)
		}
	}()
}
