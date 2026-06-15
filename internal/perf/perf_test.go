package perf

import (
	"sort"
	"sync"
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

// saveGlobals snapshots the mutable package-level perf state and restores it on
// test cleanup, so tests that drive Init/Reset against the real environment
// never leak into one another. The initOnce guard is re-armed on cleanup (it is
// a sync.Once and cannot be copied) so a later Init in the suite still runs.
func saveGlobals(t *testing.T) {
	t.Helper()
	prevEnabled := enabled.Load()
	prevInterval := logInterval.Load()
	prevLast := lastLog.Load()
	t.Cleanup(func() {
		enabled.Store(prevEnabled)
		logInterval.Store(prevInterval)
		lastLog.Store(prevLast)
		initOnce = sync.Once{}
		resetPerfState()
	})
}

func TestInit(t *testing.T) {
	tests := []struct {
		name         string
		profile      string
		intervalMs   string
		wantEnabled  bool
		wantInterval time.Duration
	}{
		{
			name:         "disabled by default",
			profile:      "",
			intervalMs:   "",
			wantEnabled:  false,
			wantInterval: defaultIntervalMs * time.Millisecond,
		},
		{
			name:         "enabled with custom interval",
			profile:      "1",
			intervalMs:   "750",
			wantEnabled:  true,
			wantInterval: 750 * time.Millisecond,
		},
		{
			name:         "explicit false stays disabled",
			profile:      "false",
			intervalMs:   "100",
			wantEnabled:  false,
			wantInterval: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveGlobals(t)
			t.Setenv("AMUX_PROFILE", tt.profile)
			t.Setenv("AMUX_PROFILE_INTERVAL_MS", tt.intervalMs)

			// Re-arm the once guard so Init actually reads the environment.
			initOnce = sync.Once{}
			Init()

			if got := Enabled(); got != tt.wantEnabled {
				t.Fatalf("after Init, Enabled()=%v, want %v", got, tt.wantEnabled)
			}
			if got := time.Duration(logInterval.Load()); got != tt.wantInterval {
				t.Fatalf("after Init, logInterval=%s, want %s", got, tt.wantInterval)
			}
		})
	}
}

func TestInitIsIdempotent(t *testing.T) {
	saveGlobals(t)

	t.Setenv("AMUX_PROFILE", "1")
	t.Setenv("AMUX_PROFILE_INTERVAL_MS", "500")
	initOnce = sync.Once{}
	Init()
	if !Enabled() {
		t.Fatalf("expected Init to arm profiling")
	}

	// A second Init must not re-read the environment (the once guard is spent),
	// so flipping the env here should have no effect.
	t.Setenv("AMUX_PROFILE", "0")
	t.Setenv("AMUX_PROFILE_INTERVAL_MS", "9000")
	Init()
	if !Enabled() {
		t.Fatalf("second Init must be a no-op; Enabled() flipped to false")
	}
	if got := time.Duration(logInterval.Load()); got != 500*time.Millisecond {
		t.Fatalf("second Init must be a no-op; interval changed to %s", got)
	}
}

func TestReset(t *testing.T) {
	tests := []struct {
		name         string
		profile      string
		intervalMs   string
		wantEnabled  bool
		wantInterval time.Duration
	}{
		{
			name:         "re-reads enabled env",
			profile:      "yes",
			intervalMs:   "321",
			wantEnabled:  true,
			wantInterval: 321 * time.Millisecond,
		},
		{
			name:         "re-reads disabled env with default interval",
			profile:      "no",
			intervalMs:   "",
			wantEnabled:  false,
			wantInterval: defaultIntervalMs * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveGlobals(t)

			// Seed some state and a stale lastLog that Reset must clear.
			enabled.Store(true)
			logInterval.Store(0)
			resetPerfState()
			Record("seeded", 7*time.Millisecond)
			Count("seededCounter", 3)
			lastLog.Store(time.Now().UnixNano())

			t.Setenv("AMUX_PROFILE", tt.profile)
			t.Setenv("AMUX_PROFILE_INTERVAL_MS", tt.intervalMs)
			Reset()

			statsMu.Lock()
			statCount := len(statsMap)
			statsMu.Unlock()
			countersMu.Lock()
			counterCount := len(counterMap)
			countersMu.Unlock()
			if statCount != 0 || counterCount != 0 {
				t.Fatalf("Reset must clear maps, got stats=%d counters=%d", statCount, counterCount)
			}
			if lastLog.Load() != 0 {
				t.Fatalf("Reset must zero lastLog, got %d", lastLog.Load())
			}
			if got := Enabled(); got != tt.wantEnabled {
				t.Fatalf("after Reset, Enabled()=%v, want %v", got, tt.wantEnabled)
			}
			if got := time.Duration(logInterval.Load()); got != tt.wantInterval {
				t.Fatalf("after Reset, logInterval=%s, want %s", got, tt.wantInterval)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		set  bool
	}{
		{name: "reports enabled", set: true},
		{name: "reports disabled", set: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveGlobals(t)
			enabled.Store(tt.set)
			if got := Enabled(); got != tt.set {
				t.Fatalf("Enabled()=%v, want %v", got, tt.set)
			}
		})
	}
}

func TestTimeRecordsWhenEnabled(t *testing.T) {
	withPerfConfig(t, true, 0)

	stop := Time("op")
	if stop == nil {
		t.Fatalf("Time must return a non-nil closure")
	}
	stop()

	stats, _ := snapshotAndReset()
	if len(stats) != 1 {
		t.Fatalf("expected exactly one recorded stat, got %d", len(stats))
	}
	if stats[0].name != "op" {
		t.Fatalf("expected stat name %q, got %q", "op", stats[0].name)
	}
	if stats[0].count != 1 {
		t.Fatalf("expected count=1 after one Time stop, got %d", stats[0].count)
	}
	if stats[0].avg < 0 || stats[0].max < 0 {
		t.Fatalf("expected non-negative durations, got avg=%s max=%s", stats[0].avg, stats[0].max)
	}
	if stats[0].avg > stats[0].max {
		t.Fatalf("expected avg <= max, got avg=%s max=%s", stats[0].avg, stats[0].max)
	}
}

func TestTimeIsNoOpWhenDisabled(t *testing.T) {
	withPerfConfig(t, false, 0)

	stop := Time("op")
	if stop == nil {
		t.Fatalf("Time must return a non-nil closure even when disabled")
	}
	stop() // must not record anything

	stats, counters := snapshotAndReset()
	if len(stats) != 0 || len(counters) != 0 {
		t.Fatalf("disabled Time must record nothing, got stats=%d counters=%d", len(stats), len(counters))
	}
}

func TestFlush(t *testing.T) {
	tests := []struct {
		name       string
		enabledVal bool
		seed       bool
		reason     string
	}{
		{name: "disabled is a no-op even with data", enabledVal: false, seed: true, reason: "shutdown"},
		{name: "enabled but empty does not panic", enabledVal: true, seed: false, reason: ""},
		{name: "enabled with data and reason resets state", enabledVal: true, seed: true, reason: "shutdown"},
		{name: "enabled with data and blank reason resets state", enabledVal: true, seed: true, reason: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withPerfConfig(t, true, 0)
			if tt.seed {
				Record("flushed", 5*time.Millisecond)
				Count("flushedCount", 4)
			}
			// Set the enabled flag under test only after seeding, so seeding
			// always lands regardless of the case's enabled value.
			enabled.Store(tt.enabledVal)

			Flush(tt.reason)

			stats, counters := snapshotAndReset()
			if tt.enabledVal {
				// Enabled Flush drains everything, so a follow-up snapshot is empty.
				if len(stats) != 0 || len(counters) != 0 {
					t.Fatalf("enabled Flush must drain state, leftover stats=%d counters=%d", len(stats), len(counters))
				}
				return
			}
			// Disabled Flush leaves the seeded data intact for a later drain.
			if !tt.seed {
				return
			}
			if len(stats) != 1 || len(counters) != 1 {
				t.Fatalf("disabled Flush must leave data, got stats=%d counters=%d", len(stats), len(counters))
			}
		})
	}
}
