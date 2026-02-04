package app

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func (a *App) markInput() {
	a.lastInputAt = time.Now()
	a.pendingInputLatency = true
}

// IsTmuxAvailable returns whether tmux is installed and available.
func (a *App) IsTmuxAvailable() bool {
	return a.tmuxAvailable
}

func (a *App) tmuxSyncWorkspaces() []*data.Workspace {
	if a.activeWorkspace != nil {
		return []*data.Workspace{a.activeWorkspace}
	}
	return nil
}
