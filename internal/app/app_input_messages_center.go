package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleOpenDiff handles the OpenDiff message.
func (a *App) handleOpenDiff(msg messages.OpenDiff) tea.Cmd {
	logging.Info("Opening diff: change=%v", msg.Change)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return tea.Batch(cmd, a.focusPane(messages.PaneCenter))
}

// handleLaunchAgent handles the LaunchAgent message.
func (a *App) handleLaunchAgent(msg messages.LaunchAgent) tea.Cmd {
	logging.Info("Launching agent: %s", msg.Assistant)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleTabCreated handles the TabCreated message.
func (a *App) handleTabCreated(msg messages.TabCreated) tea.Cmd {
	logging.Info("Tab created: %s", msg.Name)
	cmd := a.center.StartPTYReaders()
	if a.center != nil && a.center.HasDiffViewer() {
		a.setFocusedPane(messages.PaneCenter)
		return cmd
	}
	return tea.Batch(cmd, a.focusPane(messages.PaneCenter))
}
