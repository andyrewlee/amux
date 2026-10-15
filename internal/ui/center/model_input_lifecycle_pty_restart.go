package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// updatePTYStopped handles PTYStopped.
func (m *Model) updatePTYStopped(msg PTYStopped) tea.Cmd {
	var cmds []tea.Cmd
	var tagSessionName string
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "PTYStopped")
	if tab != nil {
		msg.WorkspaceID = wsID
		var stale bool
		withMu(&tab.mu, func() {
			stale = tab.State.ReaderGen != msg.Gen
		})
		if stale {
			logging.Debug("Dropping stale PTYStopped for tab %s: a newer reader already runs", msg.TabID)
			return nil
		}
		termAlive := tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		m.stopPTYReader(tab)
		withMu(&tab.mu, func() {
			if tab.Terminal != nil && len(tab.NoiseTrailing) > 0 {
				trailing := ptyio.DrainKnownPTYNoiseTrailing(&tab.NoiseTrailing)
				flushDone := perf.Time("pty_flush")
				tab.Terminal.Write(trailing)
				flushDone()
				perf.Count("pty_flush_bytes", int64(len(trailing)))
				tagSessionName, _, _ = m.noteVisibleActivityLocked(tab, false, tab.pendingVisibleSeq)
			}
		})
		if tagSessionName != "" {
			m.markActivityTagForFlush(tagSessionName)
		}
		tab.resetActivityANSIState()
		var backoff time.Duration
		shouldRestart := withMuVal(&tab.mu, func() bool {
			restart, b := tab.State.DecidePTYRestartLocked(termAlive, ptyio.PtyRestartWindow, ptyio.PtyRestartMax)
			backoff = b
			if !restart {
				tab.markDetachedLocked()
			}
			return restart
		})
		switch {
		case shouldRestart:
			tabID := msg.TabID
			wtID := msg.WorkspaceID
			cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
				return PTYRestart{WorkspaceID: wtID, TabID: tabID, Gen: msg.Gen}
			}))
			logging.Warn("PTY stopped for tab %s; restarting in %s: %v", msg.TabID, backoff, msg.Err)
		case termAlive:
			logging.Error("PTY stopped for tab %s; restart limit reached, marking detached: %v", msg.TabID, msg.Err)
			cmds = append(cmds, func() tea.Msg {
				return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
			})
		default:
			logging.Info("PTY stopped for tab %s, marking detached: %v", msg.TabID, msg.Err)
			cmds = append(cmds, func() tea.Msg {
				return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
			})
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYRestart handles PTYRestart.
func (m *Model) updatePTYRestart(msg PTYRestart) tea.Cmd {
	var cmds []tea.Cmd
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "PTYRestart")
	if tab == nil {
		return nil
	}
	var stale bool
	withMu(&tab.mu, func() {
		stale = tab.State.ReaderGen != msg.Gen
	})
	if stale {
		logging.Debug("Dropping stale PTYRestart for tab %s: a newer reader already runs", msg.TabID)
		return nil
	}
	tab.resetActivityANSIState()
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		withMu(&tab.mu, func() {
			tab.RestartBackoff = 0
		})
		return nil
	}
	if cmd := m.startPTYReader(wsID, tab); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}
