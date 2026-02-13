package center

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestFlushTiming_InactiveBackpressureRespectsHardCap(t *testing.T) {
	m := newTestModel()

	ws := newTestWorkspace("ws-main", "/repo/ws-main")
	wsID := string(ws.ID())
	width, height := 80, 24
	tab := &Tab{
		ID:            TabID("tab-main"),
		Workspace:     ws,
		Terminal:      vterm.New(width, height),
		pendingOutput: make([]byte, ptyBackpressureMultiplier*width*height+1),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	heavyWS := newTestWorkspace("ws-heavy", "/repo/ws-heavy")
	heavyWSID := string(heavyWS.ID())
	busyTabs := make([]*Tab, 0, ptyVeryHeavyLoadTabThreshold)
	for i := 0; i < ptyVeryHeavyLoadTabThreshold; i++ {
		busyTabs = append(busyTabs, &Tab{
			ID:            TabID(fmt.Sprintf("tab-busy-%d", i)),
			Workspace:     heavyWS,
			pendingOutput: []byte{'x'},
		})
	}
	m.tabsByWorkspace[heavyWSID] = busyTabs

	quiet, maxInterval := m.flushTiming(tab, false)
	if quiet != ptyFlushInactiveMaxIntervalCap {
		t.Fatalf("expected quiet=%s under extreme load cap, got %s", ptyFlushInactiveMaxIntervalCap, quiet)
	}
	if maxInterval != ptyFlushInactiveMaxIntervalCap {
		t.Fatalf("expected maxInterval=%s under extreme load cap, got %s", ptyFlushInactiveMaxIntervalCap, maxInterval)
	}
}
