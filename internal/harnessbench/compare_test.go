package harnessbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app"
)

func TestRunCompareWithRunner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	baselineFile := filepath.Join(dir, "perf.env")
	prefix := strings.ToUpper(runtime.GOOS) + "_" + strings.ToUpper(runtime.GOARCH)
	if err := os.WriteFile(baselineFile, []byte(strings.Join([]string{
		prefix + "_CENTER_P95_MS=1.0",
		prefix + "_SIDEBAR_P95_MS=5.0",
		prefix + "_MONITOR_P95_MS=1.0",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write baseline file: %v", err)
	}

	got, err := runCompareWithRunner(CompareOptions{
		BaselineFile: baselineFile,
		Tolerance:    0.10,
	}, func(cfg Config) (Report, error) {
		p95 := map[string]time.Duration{
			app.HarnessCenter:  2 * time.Millisecond,
			app.HarnessSidebar: 4 * time.Millisecond,
			app.HarnessMonitor: 2 * time.Millisecond,
		}[cfg.Mode]
		return Report{Stats: Stats{P95: p95}}, nil
	})
	if err != nil {
		t.Fatalf("runCompareWithRunner: %v", err)
	}
	if len(got.Presets) != 3 {
		t.Fatalf("len(presets) = %d, want 3", len(got.Presets))
	}
	if got.Failures() != 2 {
		t.Fatalf("failures = %d, want 2", got.Failures())
	}
	if got.Presets[1].Exceeded {
		t.Fatalf("sidebar preset unexpectedly exceeded threshold")
	}
}

func TestLoadBaselinesRejectsBadLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	baselineFile := filepath.Join(dir, "perf.env")
	if err := os.WriteFile(baselineFile, []byte("NOT_VALID\n"), 0o644); err != nil {
		t.Fatalf("write baseline file: %v", err)
	}

	if _, err := loadBaselines(baselineFile); err == nil {
		t.Fatal("expected loadBaselines to fail")
	}
}
