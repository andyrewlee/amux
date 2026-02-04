package perf

import (
	"sort"
	"testing"
	"time"
)

func resetPerfState() {
	statsMu.Lock()
	statsMap = map[string]*stat{}
	statsMu.Unlock()

	countersMu.Lock()
	counterMap = map[string]*counter{}
	countersMu.Unlock()

	lastLog.Store(0)
}

func withPerfConfig(t *testing.T, enabledValue bool, interval time.Duration) {
	t.Helper()
	prevEnabled := enabled.Load()
	prevInterval := logInterval.Load()
	enabled.Store(enabledValue)
	logInterval.Store(int64(interval))
	resetPerfState()

	t.Cleanup(func() {
		enabled.Store(prevEnabled)
		logInterval.Store(prevInterval)
		resetPerfState()
	})
}

func TestComputeP95(t *testing.T) {
	samples := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	}
	if got := computeP95(samples, len(samples), true); got != 5*time.Millisecond {
		t.Fatalf("expected p95=5ms, got %s", got)
	}

	partial := []time.Duration{9 * time.Millisecond, 1 * time.Millisecond, 5 * time.Millisecond}
	if got := computeP95(partial, 3, false); got != 9*time.Millisecond {
		t.Fatalf("expected p95=9ms for partial window, got %s", got)
	}
}

func TestSnapshotAndReset(t *testing.T) {
	withPerfConfig(t, true, 0)

	Record("b", 50*time.Millisecond)
	Record("a", 10*time.Millisecond)
	Record("b", 150*time.Millisecond)
	Count("z", 1)
	Count("y", 2)

	stats, counters := snapshotAndReset()
	if len(stats) != 2 {
		t.Fatalf("expected 2 stat snapshots, got %d", len(stats))
	}
	if len(counters) != 2 {
		t.Fatalf("expected 2 counter snapshots, got %d", len(counters))
	}

	statNames := []string{stats[0].name, stats[1].name}
	if !sort.StringsAreSorted(statNames) {
		t.Fatalf("expected stat snapshots sorted by name, got %v", statNames)
	}
	if stats[0].name != "a" || stats[0].count != 1 || stats[0].avg != 10*time.Millisecond {
		t.Fatalf("unexpected stats for a: %+v", stats[0])
	}
	if stats[1].name != "b" || stats[1].count != 2 || stats[1].min != 50*time.Millisecond || stats[1].max != 150*time.Millisecond {
		t.Fatalf("unexpected stats for b: %+v", stats[1])
	}

	counterNames := []string{counters[0].name, counters[1].name}
	if !sort.StringsAreSorted(counterNames) {
		t.Fatalf("expected counter snapshots sorted by name, got %v", counterNames)
	}
	if counters[0].name != "y" || counters[0].value != 2 {
		t.Fatalf("unexpected counter for y: %+v", counters[0])
	}
	if counters[1].name != "z" || counters[1].value != 1 {
		t.Fatalf("unexpected counter for z: %+v", counters[1])
	}

	stats, counters = snapshotAndReset()
	if len(stats) != 0 || len(counters) != 0 {
		t.Fatalf("expected reset to clear snapshots, got stats=%d counters=%d", len(stats), len(counters))
	}
}

func TestIsEnabledAndIntervalEnv(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"no":    false,
		"1":     true,
		"true":  true,
		"yes":   true,
	}
	for raw, expected := range cases {
		t.Setenv("AMUX_PROFILE", raw)
		if got := isEnabled(); got != expected {
			t.Fatalf("isEnabled(%q)=%v, want %v", raw, got, expected)
		}
	}

	t.Setenv("AMUX_PROFILE_INTERVAL_MS", "")
	if got := defaultLogInterval(); got != defaultIntervalMs*time.Millisecond {
		t.Fatalf("expected default interval, got %s", got)
	}

	t.Setenv("AMUX_PROFILE_INTERVAL_MS", "250")
	if got := defaultLogInterval(); got != 250*time.Millisecond {
		t.Fatalf("expected 250ms interval, got %s", got)
	}
}
