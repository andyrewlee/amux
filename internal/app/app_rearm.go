package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// rearmCmdForPanic returns the starter command for the periodic chain that
// msg drives, or nil when msg is not a periodic-chain message.
//
// Every background chain (tickers, watchers, dialog refresh loops) re-arms
// itself only in tail position inside its handler. When a handler panics
// before that tail runs, App.Update's recover drops the cmds the handler
// built — and without this mapping, the dropped arm means the chain is dead
// for the rest of the session. The recover path calls this to restart
// exactly the chain that just died.
//
// The mappings below mirror each handler's own tail semantics:
//   - tmuxActivityTick handlers re-arm unconditionally (even stale tokens),
//     so the rearm is unconditional too.
//   - TmuxSyncTick drops stale tokens without re-arming, so the rearm is
//     guarded by the same token check — a stale tick's early-return path
//     cannot panic, but matching the guard keeps future drift honest.
//   - The run-output loop arms the next tick from runOutputRefreshedMsg using
//     the current dialog token; rearming with msg.token resumes a live chain
//     and self-drops a stale one (handleRunOutputTick rejects stale tokens).
//   - Watchers re-read their channel: the consumer that delivered msg is
//     already spent, so exactly one new consumer is armed.
//   - The dashboard spinner re-arms only while a spinner is actually
//     running (StartSpinnerIfNeeded is a no-op otherwise).
//
// SafeTick starters are independent tickers — the panicked handler's arm
// never ran (its cmds were discarded), so this arms exactly one chain.
// Non-periodic messages return nil: panic behavior for them is unchanged
// (cmds dropped, a.err surfaced, frame invalidated).
func (a *App) rearmCmdForPanic(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case messages.GitStatusTick:
		return a.startGitStatusTicker()
	case messages.OrphanGCTick:
		return a.startOrphanGCTicker()
	case messages.PTYWatchdogTick:
		return a.startPTYWatchdog()
	case messages.TmuxSyncTick:
		if m.Token == a.tmuxActivity.syncToken {
			return a.startTmuxSyncTicker()
		}
	case tmuxActivityTick:
		return a.scheduleTmuxActivityTick()
	case runOutputTickMsg:
		return a.scheduleRunOutputTick(m.token)
	case runOutputRefreshedMsg:
		return a.scheduleRunOutputTick(m.token)
	case messages.FileWatcherEvent:
		return a.startFileWatcher()
	case messages.StateWatcherEvent:
		return a.startStateWatcher()
	case dashboard.SpinnerTickMsg:
		if a.dashboard != nil {
			return a.dashboard.StartSpinnerIfNeeded()
		}
	}
	return nil
}
