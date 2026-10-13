package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// RearmForPanic returns a starter command for the periodic PTY chain msg
// drives, or nil when msg is not a periodic-chain message this model owns.
// It mirrors App.rearmCmdForPanic's contract at the model boundary: the App
// cannot rebuild each chain's arm because the delay and fencing inputs
// (flushTiming, isActiveTab, ReaderGen) live here. Each case emits exactly
// the single tick the interrupted handler would have armed — the panicked
// handler's own arm was dropped with its cmds — under the same stale/gen
// fencing, so a rearm can never double-arm a live chain. A tick, not the
// bare msg: a bare re-delivery that panics again would hot-loop, while a
// tick self-throttles.
func (m *Model) RearmForPanic(msg tea.Msg) tea.Cmd {
	switch t := msg.(type) {
	case PTYFlush:
		tab, wsID := m.resolveTabForResult(t.WorkspaceID, t.TabID, "PTYFlush rearm")
		if tab == nil || tab.isClosed() {
			// The tab is gone — the dead chain correctly stays dead.
			return nil
		}
		// Same arm shape as the FlushGate-deferred / RearmFlush tails: one
		// quiet-period tick that re-runs the gate and clears FlushScheduled.
		quiet, _ := m.flushTiming(tab, m.isActiveTab(wsID, t.TabID))
		tabID := t.TabID
		catchUp := t.CatchUp
		return common.SafeTick(quiet, func(time.Time) tea.Msg {
			return PTYFlush{WorkspaceID: wsID, TabID: tabID, CatchUp: catchUp}
		})
	case PTYStopped:
		if tab, _ := m.resolveTabForResult(t.WorkspaceID, t.TabID, "PTYStopped rearm"); tab == nil {
			return nil
		}
		// The handler's restart arm is SafeTick(backoff, → PTYRestart); the
		// exact backoff lives inside DecidePTYRestartLocked and re-reading it
		// would double-count the attempt, so the rearm uses the chain's
		// initial delay. The delivered restart re-runs the gen + liveness
		// checks and self-drops when superseded.
		return rearmPTYRestartTick(t.WorkspaceID, t.TabID, t.Gen)
	case PTYRestart:
		if tab, _ := m.resolveTabForResult(t.WorkspaceID, t.TabID, "PTYRestart rearm"); tab == nil {
			return nil
		}
		return rearmPTYRestartTick(t.WorkspaceID, t.TabID, t.Gen)
	}
	return nil
}

// rearmPTYRestartTick emits the restart chain's own arm shape — a single
// SafeTick delivering PTYRestart with the carried gen fencing intact.
func rearmPTYRestartTick(wsID string, tabID TabID, gen uint64) tea.Cmd {
	return common.SafeTick(ptyio.RestartBackoffInitial, func(time.Time) tea.Msg {
		return PTYRestart{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	})
}
