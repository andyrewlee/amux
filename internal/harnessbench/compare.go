package harnessbench

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/app"
)

type CompareOptions struct {
	BaselineFile     string
	Tolerance        float64
	Frames           int
	ScrollbackFrames int
	Warmup           int
	Width            int
	Height           int
}

type ComparePresetResult struct {
	Name         string  `json:"name"`
	BaselineKey  string  `json:"baseline_key"`
	MeasuredP95  string  `json:"measured_p95"`
	MeasuredMS   float64 `json:"measured_ms"`
	BaselineMS   float64 `json:"baseline_ms,omitempty"`
	ThresholdMS  float64 `json:"threshold_ms,omitempty"`
	BaselineSet  bool    `json:"baseline_set"`
	Exceeded     bool    `json:"exceeded"`
	Skipped      bool    `json:"skipped"`
	FailureLabel string  `json:"failure_label,omitempty"`
}

type CompareResult struct {
	GOOS      string                `json:"goos"`
	GOARCH    string                `json:"goarch"`
	Prefix    string                `json:"prefix"`
	Tolerance float64               `json:"tolerance"`
	Presets   []ComparePresetResult `json:"presets"`
}

type preset struct {
	Name   string
	Config Config
}

func RunCompare(opts CompareOptions) (CompareResult, error) {
	return runCompareWithRunner(opts, Run)
}

func runCompareWithRunner(opts CompareOptions, runner func(Config) (Report, error)) (CompareResult, error) {
	if strings.TrimSpace(opts.BaselineFile) == "" {
		return CompareResult{}, errors.New("baseline file is required")
	}
	if opts.Tolerance < 0 {
		return CompareResult{}, errors.New("tolerance must be >= 0")
	}
	baselines, err := loadBaselines(opts.BaselineFile)
	if err != nil {
		return CompareResult{}, err
	}

	prefix := strings.ToUpper(runtime.GOOS) + "_" + strings.ToUpper(runtime.GOARCH)
	result := CompareResult{
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		Prefix:    prefix,
		Tolerance: opts.Tolerance,
	}

	for _, preset := range comparePresets(opts) {
		report, err := runner(preset.Config)
		if err != nil {
			return CompareResult{}, fmt.Errorf("%s preset failed: %w", preset.Name, err)
		}
		key := prefix + "_" + preset.Name + "_P95_MS"
		entry := ComparePresetResult{
			Name:        preset.Name,
			BaselineKey: key,
			MeasuredP95: report.Stats.P95.String(),
			MeasuredMS:  durationMS(report.Stats.P95),
		}
		if baseline, ok := baselines[key]; ok {
			entry.BaselineSet = true
			entry.BaselineMS = baseline
			entry.ThresholdMS = baseline * (1 + opts.Tolerance)
			entry.Exceeded = entry.MeasuredMS > entry.ThresholdMS
			if entry.Exceeded {
				entry.FailureLabel = preset.Name + " p95 exceeded threshold"
			}
		} else {
			entry.Skipped = true
		}
		result.Presets = append(result.Presets, entry)
	}

	return result, nil
}

func (r CompareResult) Failures() int {
	failures := 0
	for _, preset := range r.Presets {
		if preset.Exceeded {
			failures++
		}
	}
	return failures
}

func comparePresets(opts CompareOptions) []preset {
	frames := opts.Frames
	if frames <= 0 {
		frames = 300
	}
	scrollbackFrames := opts.ScrollbackFrames
	if scrollbackFrames <= 0 {
		scrollbackFrames = 600
	}
	warmup := opts.Warmup
	if warmup < 0 {
		warmup = 30
	}
	width := opts.Width
	if width <= 0 {
		width = 160
	}
	height := opts.Height
	if height <= 0 {
		height = 48
	}

	return []preset{
		{
			Name: "CENTER",
			Config: Config{
				HarnessOptions: app.HarnessOptions{
					Mode:         app.HarnessCenter,
					Tabs:         16,
					HotTabs:      2,
					PayloadBytes: 64,
					Width:        width,
					Height:       height,
				},
				Frames: frames,
				Warmup: warmup,
			},
		},
		{
			Name: "SIDEBAR",
			Config: Config{
				HarnessOptions: app.HarnessOptions{
					Mode:         app.HarnessSidebar,
					Tabs:         16,
					HotTabs:      1,
					PayloadBytes: 64,
					NewlineEvery: 1,
					Width:        width,
					Height:       height,
				},
				Frames: scrollbackFrames,
				Warmup: warmup,
			},
		},
		{
			Name: "MONITOR",
			Config: Config{
				HarnessOptions: app.HarnessOptions{
					Mode:         app.HarnessMonitor,
					Tabs:         16,
					HotTabs:      4,
					PayloadBytes: 64,
					Width:        width,
					Height:       height,
				},
				Frames: frames,
				Warmup: warmup,
			},
		},
	}
}

func loadBaselines(path string) (map[string]float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening baseline file: %w", err)
	}
	defer file.Close()

	out := make(map[string]float64)
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("invalid baseline line %d", lineNo)
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid baseline value on line %d: %w", lineNo, err)
		}
		out[strings.TrimSpace(key)] = parsed
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading baseline file: %w", err)
	}
	return out, nil
}
