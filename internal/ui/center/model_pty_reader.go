package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func (m *Model) flushTiming(tab *Tab, active bool) (time.Duration, time.Duration) {
	quiet := ptyFlushQuiet
	maxInterval := ptyFlushMaxInterval

	// Snapshot terminal state under lock, then release before the load-sampling call.
	tab.mu.Lock()
	altScreen := tab.Terminal != nil && tab.Terminal.AltScreen
	var termWidth, termHeight int
	var pendingLen int
	if tab.Terminal != nil {
		termWidth = tab.Terminal.Width
		termHeight = tab.Terminal.Height
		pendingLen = len(tab.pendingOutput)
	}
	tab.mu.Unlock()

	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if altScreen {
		quiet = ptyFlushQuietAlt
		maxInterval = ptyFlushMaxAlt
	}

	// Apply backpressure when pending output exceeds threshold
	// This prevents renderer thrashing during heavy output (like builds)
	if pendingLen > 0 {
		threshold := ptyBackpressureMultiplier * termWidth * termHeight
		if pendingLen > threshold {
			// Under backpressure: use minimum flush interval
			if quiet < ptyBackpressureFlushFloor {
				quiet = ptyBackpressureFlushFloor
			}
			if maxInterval < ptyBackpressureFlushFloor {
				maxInterval = ptyBackpressureFlushFloor
			}
		}
	}

	if !active {
		busyCount := m.busyPTYTabCount(time.Now())
		var mult time.Duration
		switch {
		case busyCount >= ptyVeryHeavyLoadTabThreshold:
			mult = ptyFlushInactiveVeryHeavyMultiplier
		case busyCount >= ptyHeavyLoadTabThreshold:
			mult = ptyFlushInactiveHeavyMultiplier
		default:
			mult = ptyFlushInactiveMultiplier
		}
		quiet *= mult
		maxInterval *= mult
		if quiet > ptyFlushInactiveMaxIntervalCap {
			quiet = ptyFlushInactiveMaxIntervalCap
		}
		if maxInterval > ptyFlushInactiveMaxIntervalCap {
			maxInterval = ptyFlushInactiveMaxIntervalCap
		}
		if maxInterval < quiet {
			maxInterval = quiet
		}
	}

	return quiet, maxInterval
}

func (m *Model) busyPTYTabCount(now time.Time) int {
	// Return cached count if sampled within ptyLoadSampleInterval (100ms)
	if !m.flushLoadSampleAt.IsZero() && now.Sub(m.flushLoadSampleAt) < ptyLoadSampleInterval {
		return m.cachedBusyTabCount
	}
	count := 0
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			busy := atomic.LoadUint32(&tab.readerActiveState) == 1 || len(tab.pendingOutput) > 0
			if busy {
				count++
			}
		}
	}
	m.flushLoadSampleAt = now
	m.cachedBusyTabCount = count
	return count
}

func (m *Model) forwardPTYMsgs(msgCh <-chan tea.Msg) {
	common.ForwardPTYMsgs(msgCh, m.msgSink, common.OutputMerger{
		ExtractData: func(msg tea.Msg) ([]byte, bool) {
			if out, ok := msg.(PTYOutput); ok {
				return out.Data, true
			}
			return nil, false
		},
		CanMerge: func(cur, next tea.Msg) bool {
			c, _ := cur.(PTYOutput)
			n, _ := next.(PTYOutput)
			return c.WorkspaceID == n.WorkspaceID && c.TabID == n.TabID
		},
		Build: func(first tea.Msg, data []byte) tea.Msg {
			out, _ := first.(PTYOutput)
			out.Data = data
			return out
		},
		MaxPending: ptyMaxPendingBytes,
	})
}
