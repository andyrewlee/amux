//go:build soak

package app

import (
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func TestSoakHarnessPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	duration := soakDuration(t, 5*time.Minute)

	restore := perf.EnableForTest()
	defer restore()

	h, err := NewHarness(HarnessOptions{
		Mode:         HarnessCenter,
		Tabs:         8,
		Width:        160,
		Height:       48,
		HotTabs:      4,
		PayloadBytes: 128,
		NewlineEvery: 8,
	})
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}

	app := &App{
		externalMsgs:     make(chan tea.Msg, 64),
		externalCritical: make(chan tea.Msg, 16),
	}

	var sent int64
	app.SetMsgSender(func(msg tea.Msg) {
		_ = msg
		atomic.AddInt64(&sent, 1)
	})

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		payload := []byte("soak-pty-output")
		var i int
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			app.enqueueExternalMsg(center.PTYOutput{
				WorkspaceID: "soak",
				TabID:       center.TabID("tab-0"),
				Data:        payload,
			})
			if i%512 == 0 {
				app.enqueueExternalMsg(messages.Error{
					Err:     errors.New("soak heartbeat"),
					Context: "soak",
				})
			}
			i++
		}
	}()

	deadline := time.Now().Add(duration)
	frame := 0
	for time.Now().Before(deadline) {
		h.Step(frame)
		start := time.Now()
		_ = h.Render()
		perf.Record("soak_render", time.Since(start))
		frame++
	}

	close(stop)
	<-done
	close(app.externalMsgs)
	close(app.externalCritical)

	stats, counters := perf.Snapshot()
	logSoakPerf(t, stats, counters, sent)
}

func soakDuration(t *testing.T, fallback time.Duration) time.Duration {
	t.Helper()
	raw := os.Getenv("AMUX_SOAK_DURATION")
	if raw == "" {
		if mins := os.Getenv("AMUX_SOAK_MINUTES"); mins != "" {
			val, err := strconv.Atoi(mins)
			if err != nil || val <= 0 {
				t.Fatalf("invalid AMUX_SOAK_MINUTES=%q", mins)
			}
			return time.Duration(val) * time.Minute
		}
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		t.Fatalf("invalid AMUX_SOAK_DURATION=%q", raw)
	}
	return d
}

func logSoakPerf(t *testing.T, stats []perf.StatSnapshot, counters []perf.CounterSnapshot, sent int64) {
	t.Helper()
	if sent > 0 {
		t.Logf("soak external messages delivered=%d", sent)
	}
	for _, s := range stats {
		if s.Name == "soak_render" {
			t.Logf("soak render count=%d avg=%s p95=%s min=%s max=%s",
				s.Count, s.Avg, s.P95, s.Min, s.Max)
		}
	}
	for _, c := range counters {
		switch c.Name {
		case "external_msg_drop", "external_msg_drop_noncritical", "external_msg_drop_critical":
			t.Logf("soak counter %s=%d", c.Name, c.Value)
		}
	}
}
