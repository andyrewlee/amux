package perf

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const (
	defaultSampleWindow = 256
	defaultIntervalMs   = 5000
)

type stat struct {
	mu      sync.Mutex
	count   int64
	total   time.Duration
	min     time.Duration
	max     time.Duration
	samples []time.Duration
	idx     int
	full    bool
}

type counter struct {
	mu    sync.Mutex
	value int64
}

type statSnapshot struct {
	name  string
	count int64
	avg   time.Duration
	min   time.Duration
	max   time.Duration
	p95   time.Duration
}

type counterSnapshot struct {
	name  string
	value int64
}

type statEntry struct {
	name string
	stat *stat
}

type counterEntry struct {
	name    string
	counter *counter
}

var (
	enabled     bool
	logInterval time.Duration
	lastLog     atomic.Int64

	statsMu  sync.Mutex
	statsMap = map[string]*stat{}

	countersMu sync.Mutex
	counterMap = map[string]*counter{}
	initOnce   sync.Once
)

func init() {
	initOnce.Do(func() {
		enabled = isEnabled()
		logInterval = defaultLogInterval()
	})
}

// Enabled reports whether profiling is enabled.
func Enabled() bool {
	return enabled
}

// Time returns a function that records elapsed time when invoked.
func Time(name string) func() {
	if !enabled {
		return func() {}
	}
	start := time.Now()
	return func() {
		Record(name, time.Since(start))
	}
}

// Record captures a duration sample for the given name.
func Record(name string, d time.Duration) {
	if !enabled {
		return
	}
	s := getStat(name)
	s.mu.Lock()
	s.count++
	s.total += d
	if s.count == 1 || d < s.min {
		s.min = d
	}
	if d > s.max {
		s.max = d
	}
	if s.samples == nil {
		s.samples = make([]time.Duration, defaultSampleWindow)
	}
	s.samples[s.idx] = d
	s.idx++
	if s.idx >= len(s.samples) {
		s.idx = 0
		s.full = true
	}
	s.mu.Unlock()

	maybeLog()
}

// Count increments a named counter by delta.
func Count(name string, delta int64) {
	if !enabled {
		return
	}
	c := getCounter(name)
	c.mu.Lock()
	c.value += delta
	c.mu.Unlock()

	maybeLog()
}

func getStat(name string) *stat {
	statsMu.Lock()
	defer statsMu.Unlock()
	s, ok := statsMap[name]
	if !ok {
		s = &stat{}
		statsMap[name] = s
	}
	return s
}

func getCounter(name string) *counter {
	countersMu.Lock()
	defer countersMu.Unlock()
	c, ok := counterMap[name]
	if !ok {
		c = &counter{}
		counterMap[name] = c
	}
	return c
}

func maybeLog() {
	if !enabled || logInterval <= 0 {
		return
	}
	now := time.Now().UnixNano()
	last := lastLog.Load()
	if last != 0 && time.Duration(now-last) < logInterval {
		return
	}
	if !lastLog.CompareAndSwap(last, now) {
		return
	}
	stats, counters := snapshotAndReset()
	if len(stats) == 0 && len(counters) == 0 {
		return
	}
	for _, s := range stats {
		logging.Info(
			"PERF %s count=%d avg=%s p95=%s min=%s max=%s",
			s.name, s.count, s.avg, s.p95, s.min, s.max,
		)
	}
	for _, c := range counters {
		logging.Info("PERF %s count=%d", c.name, c.value)
	}
}

// Flush logs a summary of current stats/counters immediately.
// If reason is provided, it is included in the log prefix.
func Flush(reason string) {
	if !enabled {
		return
	}
	stats, counters := snapshotAndReset()
	if len(stats) == 0 && len(counters) == 0 {
		return
	}
	prefix := "PERF SUMMARY"
	if strings.TrimSpace(reason) != "" {
		prefix = fmt.Sprintf("PERF SUMMARY %s", reason)
	}
	for _, s := range stats {
		logging.Info(
			"%s %s count=%d avg=%s p95=%s min=%s max=%s",
			prefix, s.name, s.count, s.avg, s.p95, s.min, s.max,
		)
	}
	for _, c := range counters {
		logging.Info("%s %s count=%d", prefix, c.name, c.value)
	}
}

func snapshotAndReset() ([]statSnapshot, []counterSnapshot) {
	statsMu.Lock()
	statsList := make([]statEntry, 0, len(statsMap))
	for name, s := range statsMap {
		statsList = append(statsList, statEntry{name: name, stat: s})
	}
	statsMu.Unlock()

	snapshots := make([]statSnapshot, 0, len(statsList))
	for _, entry := range statsList {
		name := entry.name
		s := entry.stat
		s.mu.Lock()
		if s.count == 0 {
			s.mu.Unlock()
			continue
		}
		count := s.count
		avg := time.Duration(0)
		if count > 0 {
			avg = time.Duration(int64(s.total) / count)
		}
		min := s.min
		max := s.max
		p95 := computeP95(s.samples, s.idx, s.full)

		s.count = 0
		s.total = 0
		s.min = 0
		s.max = 0
		s.idx = 0
		s.full = false
		s.mu.Unlock()

		snapshots = append(snapshots, statSnapshot{
			name:  name,
			count: count,
			avg:   avg,
			min:   min,
			max:   max,
			p95:   p95,
		})
	}

	countersMu.Lock()
	counterList := make([]counterEntry, 0, len(counterMap))
	for name, c := range counterMap {
		counterList = append(counterList, counterEntry{name: name, counter: c})
	}
	countersMu.Unlock()

	counterSnapshots := make([]counterSnapshot, 0, len(counterList))
	for _, entry := range counterList {
		name := entry.name
		c := entry.counter
		c.mu.Lock()
		value := c.value
		c.value = 0
		c.mu.Unlock()
		if value == 0 {
			continue
		}
		counterSnapshots = append(counterSnapshots, counterSnapshot{
			name:  name,
			value: value,
		})
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].name < snapshots[j].name
	})
	sort.Slice(counterSnapshots, func(i, j int) bool {
		return counterSnapshots[i].name < counterSnapshots[j].name
	})

	return snapshots, counterSnapshots
}

func computeP95(samples []time.Duration, idx int, full bool) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var n int
	if full {
		n = len(samples)
	} else {
		n = idx
	}
	if n == 0 {
		return 0
	}
	window := make([]time.Duration, n)
	copy(window, samples[:n])
	sort.Slice(window, func(i, j int) bool {
		return window[i] < window[j]
	})
	pos := int(math.Ceil(0.95*float64(n))) - 1
	if pos < 0 {
		pos = 0
	}
	if pos >= n {
		pos = n - 1
	}
	return window[pos]
}

func isEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("AMUX_PROFILE"))
	if raw == "" {
		return false
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
}

func defaultLogInterval() time.Duration {
	interval := defaultIntervalMs
	if raw := strings.TrimSpace(os.Getenv("AMUX_PROFILE_INTERVAL_MS")); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			interval = val
		}
	}
	return time.Duration(interval) * time.Millisecond
}
