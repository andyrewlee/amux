package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleOpenDiff handles the OpenDiff message.
func (a *App) handleOpenDiff(msg messages.OpenDiff) tea.Cmd {
	logging.Info("Opening diff: %s", msg.File)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
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
	a.focusPane(messages.PaneCenter)
	return common.SafeBatch(cmd, a.enforceAttachedTabLimit())
}
