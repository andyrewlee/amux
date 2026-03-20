package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/harnessbench"
)

type devPerfCompareResult struct {
	GOOS      string                             `json:"goos"`
	GOARCH    string                             `json:"goarch"`
	Prefix    string                             `json:"prefix"`
	Tolerance float64                            `json:"tolerance"`
	Presets   []harnessbench.ComparePresetResult `json:"presets"`
	Failures  int                                `json:"failures"`
}

func cmdDevPerfCompare(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux dev perf-compare [--baseline-file <path>] [--tolerance <ratio>] [--frames <n>] [--scrollback-frames <n>] [--warmup <n>] [--width <n>] [--height <n>]"

	fs := newFlagSet("dev perf-compare")
	baselineFile := fs.String("baseline-file", envOrDefault("PERF_BASELINE_FILE", "scripts/perf_baselines.env"), "baseline file path")
	tolerance := fs.String("tolerance", envOrDefault("PERF_TOLERANCE", "0.10"), "allowed regression ratio")
	frames := fs.Int("frames", envInt("HARNESS_FRAMES", 300), "measured frames for center/monitor presets")
	scrollbackFrames := fs.Int("scrollback-frames", envInt("HARNESS_SCROLLBACK_FRAMES", 600), "measured frames for sidebar preset")
	warmup := fs.Int("warmup", envInt("HARNESS_WARMUP", 30), "warmup frames to ignore")
	width := fs.Int("width", envInt("HARNESS_WIDTH", 160), "screen width")
	height := fs.Int("height", envInt("HARNESS_HEIGHT", 48), "screen height")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	if strings.TrimSpace(*baselineFile) == "" {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--baseline-file is required"))
	}
	parsedTolerance, err := strconv.ParseFloat(strings.TrimSpace(*tolerance), 64)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("invalid --tolerance: %w", err))
	}
	if parsedTolerance < 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--tolerance must be >= 0"))
	}
	if *frames <= 0 || *scrollbackFrames <= 0 || *width <= 0 || *height <= 0 || *warmup < 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("frames, scrollback-frames, width, height must be > 0 and warmup must be >= 0"))
	}

	result, err := harnessbench.RunCompare(harnessbench.CompareOptions{
		BaselineFile:     *baselineFile,
		Tolerance:        parsedTolerance,
		Frames:           *frames,
		ScrollbackFrames: *scrollbackFrames,
		Warmup:           *warmup,
		Width:            *width,
		Height:           *height,
	})
	if err != nil {
		if gf.JSON {
			ReturnError(w, "perf_compare_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "%v", err)
		}
		return ExitInternalError
	}

	failures := result.Failures()
	if gf.JSON {
		PrintJSON(w, devPerfCompareResult{
			GOOS:      result.GOOS,
			GOARCH:    result.GOARCH,
			Prefix:    result.Prefix,
			Tolerance: result.Tolerance,
			Presets:   result.Presets,
			Failures:  failures,
		}, version)
		if failures > 0 {
			return ExitInternalError
		}
		return ExitOK
	}

	for _, preset := range result.Presets {
		fmt.Fprintf(w, "Running %s preset...\n", preset.Name)
		if preset.BaselineSet {
			fmt.Fprintf(w, "%s p95: measured=%.6fms baseline=%.6fms threshold=%.6fms\n",
				preset.Name, preset.MeasuredMS, preset.BaselineMS, preset.ThresholdMS)
		} else {
			fmt.Fprintf(w, "No baseline set for %s; skipping comparison.\n", preset.BaselineKey)
		}
		if preset.Exceeded {
			fmt.Fprintf(wErr, "%s\n", preset.FailureLabel)
		}
	}
	if failures > 0 {
		fmt.Fprintf(wErr, "Perf comparison failed (%d preset(s) over threshold).\n", failures)
		return ExitInternalError
	}
	fmt.Fprintln(w, "Perf comparison passed.")
	return ExitOK
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
