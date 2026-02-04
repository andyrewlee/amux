package perf

import "time"

// StatSnapshot captures perf duration stats for diagnostics/tests.
type StatSnapshot struct {
	Name  string
	Count int64
	Avg   time.Duration
	Min   time.Duration
	Max   time.Duration
	P95   time.Duration
}

// CounterSnapshot captures perf counters for diagnostics/tests.
type CounterSnapshot struct {
	Name  string
	Value int64
}

// EnableForTest forces perf collection on (and disables periodic logging).
// It returns a restore function to reset prior settings.
func EnableForTest() func() {
	prevEnabled := enabled.Load()
	prevInterval := logInterval.Load()
	enabled.Store(true)
	logInterval.Store(0)
	lastLog.Store(0)
	return func() {
		enabled.Store(prevEnabled)
		logInterval.Store(prevInterval)
	}
}

// Snapshot returns current perf stats/counters and resets them.
func Snapshot() ([]StatSnapshot, []CounterSnapshot) {
	stats, counters := snapshotAndReset()
	statsOut := make([]StatSnapshot, 0, len(stats))
	for _, s := range stats {
		statsOut = append(statsOut, StatSnapshot{
			Name:  s.name,
			Count: s.count,
			Avg:   s.avg,
			Min:   s.min,
			Max:   s.max,
			P95:   s.p95,
		})
	}
	counterOut := make([]CounterSnapshot, 0, len(counters))
	for _, c := range counters {
		counterOut = append(counterOut, CounterSnapshot{
			Name:  c.name,
			Value: c.value,
		})
	}
	return statsOut, counterOut
}
