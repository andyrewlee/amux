package harnessbench

import (
	"errors"
	"time"

	"github.com/andyrewlee/amux/internal/app"
)

type Config struct {
	app.HarnessOptions
	Frames int
	Warmup int
}

type Stats struct {
	Avg time.Duration
	Min time.Duration
	Max time.Duration
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
}

type Report struct {
	Config Config
	Total  time.Duration
	Stats  Stats
	FPS    float64
}

func Run(cfg Config) (Report, error) {
	if cfg.Frames <= 0 {
		return Report{}, errors.New("frames must be > 0")
	}
	if cfg.Warmup < 0 {
		return Report{}, errors.New("warmup must be >= 0")
	}

	h, err := app.NewHarness(cfg.HarnessOptions)
	if err != nil {
		return Report{}, err
	}

	totalFrames := cfg.Warmup + cfg.Frames
	durations := make([]time.Duration, 0, cfg.Frames)
	startAll := time.Now()

	for i := 0; i < totalFrames; i++ {
		h.Step(i)
		start := time.Now()
		view := h.Render()
		_ = view.Content
		if i >= cfg.Warmup {
			durations = append(durations, time.Since(start))
		}
	}

	return Report{
		Config: cfg,
		Total:  time.Since(startAll),
		Stats:  summarize(durations),
		FPS:    fps(durations),
	}, nil
}

func summarize(durations []time.Duration) Stats {
	if len(durations) == 0 {
		return Stats{}
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sortDurations(sorted)

	var total time.Duration
	for _, d := range durations {
		total += d
	}

	return Stats{
		Avg: total / time.Duration(len(durations)),
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: percentile(sorted, 0.50),
		P95: percentile(sorted, 0.95),
		P99: percentile(sorted, 0.99),
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

func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
