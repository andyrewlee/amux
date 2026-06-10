package center

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// updatePTYStopped handles PTYStopped.
func (m *Model) updatePTYStopped(msg PTYStopped) tea.Cmd {
	var cmds []tea.Cmd
	var tagSessionName string
	var tagTimestamp int64
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil {
		termAlive := tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		m.stopPTYReader(tab)
		tab.mu.Lock()
		if tab.Terminal != nil && len(tab.NoiseTrailing) > 0 {
			trailing := ptyio.DrainKnownPTYNoiseTrailing(&tab.NoiseTrailing)
			flushDone := perf.Time("pty_flush")
			tab.Terminal.Write(trailing)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(trailing)))
			tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLocked(tab, false, tab.pendingVisibleSeq)
		}
		tab.mu.Unlock()
		if tagSessionName != "" {
			opts := m.tmuxOpts
			sessionName := tagSessionName
			timestamp := strconv.FormatInt(tagTimestamp, 10)
			cmds = append(cmds, func() tea.Msg {
				_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, timestamp, opts)
				return nil
			})
		}
		tab.resetActivityANSIState()
		if termAlive {
			tab.mu.Lock()
			backoff, shouldRestart := tab.State.NextRestartBackoffLocked(ptyRestartWindow, ptyRestartMax)
			if !shouldRestart {
				tab.markDetachedLocked()
			}
			tab.mu.Unlock()
			if shouldRestart {
				tabID := msg.TabID
				wtID := msg.WorkspaceID
				cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
					return PTYRestart{WorkspaceID: wtID, TabID: tabID}
				}))
				logging.Warn("PTY stopped for tab %s; restarting in %s: %v", msg.TabID, backoff, msg.Err)
			} else {
				logging.Warn("PTY stopped for tab %s; restart limit reached, marking detached: %v", msg.TabID, msg.Err)
				cmds = append(cmds, func() tea.Msg {
					return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
				})
			}
		} else {
			tab.mu.Lock()
			tab.markDetachedLocked()
			tab.State.ResetRestartBackoffLocked()
			tab.mu.Unlock()
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
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	tab.resetActivityANSIState()
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.mu.Lock()
		tab.RestartBackoff = 0
		tab.mu.Unlock()
		return nil
	}
	if cmd := m.startPTYReader(msg.WorkspaceID, tab); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}
