package perf

import (
	"testing"
	"time"
)

// TestEnableForTest verifies that EnableForTest forces collection on, disables
// periodic logging, zeroes lastLog, and returns a restore closure that puts the
// prior enabled/interval settings back. The cases seed a variety of prior
// states (including the no-op "already enabled" case) so the save/restore round
// trip is exercised across boundaries.
func TestEnableForTest(t *testing.T) {
	tests := []struct {
		name         string
		prevEnabled  bool
		prevInterval time.Duration
		prevLastLog  int64
	}{
		{
			name:         "from disabled with default interval",
			prevEnabled:  false,
			prevInterval: defaultIntervalMs * time.Millisecond,
			prevLastLog:  0,
		},
		{
			name:         "from enabled with custom interval and stale lastLog",
			prevEnabled:  true,
			prevInterval: 750 * time.Millisecond,
			prevLastLog:  1234567890,
		},
		{
			name:         "from disabled with zero interval already",
			prevEnabled:  false,
			prevInterval: 0,
			prevLastLog:  42,
		},
		{
			name:         "from enabled with zero interval",
			prevEnabled:  true,
			prevInterval: 0,
			prevLastLog:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveGlobals(t)

			enabled.Store(tt.prevEnabled)
			logInterval.Store(int64(tt.prevInterval))
			lastLog.Store(tt.prevLastLog)

			restore := EnableForTest()
			if restore == nil {
				t.Fatalf("EnableForTest must return a non-nil restore closure")
			}

			// While the test mode is active, collection is forced on, periodic
			// logging is disabled, and lastLog is cleared.
			if !enabled.Load() {
				t.Fatalf("EnableForTest must force enabled=true, got false")
			}
			if got := logInterval.Load(); got != 0 {
				t.Fatalf("EnableForTest must zero logInterval, got %d", got)
			}
			if got := lastLog.Load(); got != 0 {
				t.Fatalf("EnableForTest must zero lastLog, got %d", got)
			}

			// Recording while active must actually capture a sample, proving
			// the forced-on flag takes effect end to end.
			Record("during-test", 3*time.Millisecond)
			stats, _ := snapshotAndReset()
			if len(stats) != 1 || stats[0].name != "during-test" {
				t.Fatalf("expected one sample recorded while EnableForTest active, got %+v", stats)
			}

			// The restore closure must put the prior enabled/interval settings
			// back exactly. (It intentionally does not restore lastLog.)
			restore()
			if got := enabled.Load(); got != tt.prevEnabled {
				t.Fatalf("restore must reset enabled to %v, got %v", tt.prevEnabled, got)
			}
			if got := logInterval.Load(); got != int64(tt.prevInterval) {
				t.Fatalf("restore must reset logInterval to %d, got %d", int64(tt.prevInterval), got)
			}
		})
	}
}

// TestEnableForTestRestoreIsIdempotent verifies the restore closure can be
// called more than once without changing the restored state further.
func TestEnableForTestRestoreIsIdempotent(t *testing.T) {
	saveGlobals(t)

	enabled.Store(true)
	logInterval.Store(int64(500 * time.Millisecond))

	restore := EnableForTest()
	restore()

	gotEnabled := enabled.Load()
	gotInterval := logInterval.Load()

	restore() // second call must be a stable no-op against the restored values
	if enabled.Load() != gotEnabled {
		t.Fatalf("second restore changed enabled from %v to %v", gotEnabled, enabled.Load())
	}
	if logInterval.Load() != gotInterval {
		t.Fatalf("second restore changed interval from %d to %d", gotInterval, logInterval.Load())
	}
}

// TestSnapshot exercises the public Snapshot wrapper: it must translate the
// internal stat/counter snapshots into the exported StatSnapshot/CounterSnapshot
// structs (carrying every field through), sort by name, drain zero-valued
// entries, and reset state so a second call returns empty slices.
func TestSnapshot(t *testing.T) {
	tests := []struct {
		name         string
		stats        map[string][]time.Duration
		counters     map[string]int64
		wantStats    []StatSnapshot
		wantCounters []CounterSnapshot
	}{
		{
			name:         "no recorded data yields empty non-nil slices",
			stats:        nil,
			counters:     nil,
			wantStats:    []StatSnapshot{},
			wantCounters: []CounterSnapshot{},
		},
		{
			name: "single stat carries every field through",
			stats: map[string][]time.Duration{
				"solo": {10 * time.Millisecond},
			},
			wantStats: []StatSnapshot{
				{
					Name:  "solo",
					Count: 1,
					Avg:   10 * time.Millisecond,
					Min:   10 * time.Millisecond,
					Max:   10 * time.Millisecond,
					P95:   10 * time.Millisecond,
				},
			},
			wantCounters: []CounterSnapshot{},
		},
		{
			name: "multiple stats and counters sorted by name",
			stats: map[string][]time.Duration{
				"b": {50 * time.Millisecond, 150 * time.Millisecond},
				"a": {10 * time.Millisecond},
			},
			counters: map[string]int64{
				"z": 1,
				"y": 2,
			},
			wantStats: []StatSnapshot{
				{
					Name:  "a",
					Count: 1,
					Avg:   10 * time.Millisecond,
					Min:   10 * time.Millisecond,
					Max:   10 * time.Millisecond,
					P95:   10 * time.Millisecond,
				},
				{
					Name:  "b",
					Count: 2,
					Avg:   100 * time.Millisecond,
					Min:   50 * time.Millisecond,
					Max:   150 * time.Millisecond,
					P95:   150 * time.Millisecond,
				},
			},
			wantCounters: []CounterSnapshot{
				{Name: "y", Value: 2},
				{Name: "z", Value: 1},
			},
		},
		{
			name:      "counters only, no stats",
			counters:  map[string]int64{"only-counter": 7},
			wantStats: []StatSnapshot{},
			wantCounters: []CounterSnapshot{
				{Name: "only-counter", Value: 7},
			},
		},
		{
			name: "negative counter delta is preserved (non-zero, so not drained)",
			counters: map[string]int64{
				"neg": -5,
			},
			wantStats: []StatSnapshot{},
			wantCounters: []CounterSnapshot{
				{Name: "neg", Value: -5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withPerfConfig(t, true, 0)

			for name, durations := range tt.stats {
				for _, d := range durations {
					Record(name, d)
				}
			}
			for name, delta := range tt.counters {
				Count(name, delta)
			}

			stats, counters := Snapshot()

			assertStatSnapshots(t, stats, tt.wantStats)
			assertCounterSnapshots(t, counters, tt.wantCounters)

			// Snapshot drains state: a follow-up call must return empty slices.
			stats, counters = Snapshot()
			if len(stats) != 0 {
				t.Fatalf("second Snapshot must drain stats, got %d", len(stats))
			}
			if len(counters) != 0 {
				t.Fatalf("second Snapshot must drain counters, got %d", len(counters))
			}
		})
	}
}

// TestSnapshotDropsZeroValuedEntries verifies that a counter incremented then
// decremented back to zero is not surfaced by Snapshot, mirroring the drain
// semantics of snapshotAndReset.
func TestSnapshotDropsZeroValuedEntries(t *testing.T) {
	withPerfConfig(t, true, 0)

	Count("nets-to-zero", 3)
	Count("nets-to-zero", -3)
	Count("survives", 1)

	stats, counters := Snapshot()
	if len(stats) != 0 {
		t.Fatalf("expected no stats, got %d", len(stats))
	}
	if len(counters) != 1 {
		t.Fatalf("expected only the non-zero counter to survive, got %d: %+v", len(counters), counters)
	}
	if counters[0].Name != "survives" || counters[0].Value != 1 {
		t.Fatalf("unexpected surviving counter: %+v", counters[0])
	}
}

// TestSnapshotIgnoresDataWhenDisabled verifies that with collection disabled,
// Record/Count are no-ops, so Snapshot returns empty slices.
func TestSnapshotIgnoresDataWhenDisabled(t *testing.T) {
	withPerfConfig(t, false, 0)

	Record("ignored", 5*time.Millisecond)
	Count("ignored-counter", 9)

	stats, counters := Snapshot()
	if len(stats) != 0 || len(counters) != 0 {
		t.Fatalf("disabled collection must yield empty snapshot, got stats=%d counters=%d", len(stats), len(counters))
	}
}

func assertStatSnapshots(t *testing.T, got, want []StatSnapshot) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d stat snapshots, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stat snapshot %d mismatch:\n got=%+v\nwant=%+v", i, got[i], want[i])
		}
	}
}

func assertCounterSnapshots(t *testing.T, got, want []CounterSnapshot) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d counter snapshots, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("counter snapshot %d mismatch:\n got=%+v\nwant=%+v", i, got[i], want[i])
		}
	}
}
