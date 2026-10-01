package app

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func (a *App) markInput() {
	a.lastInputAt = time.Now()
	a.pendingInputLatency = true
}

func (a *App) tmuxSyncWorkspaces() []*data.Workspace {
	if a.activeWorkspace != nil {
		return []*data.Workspace{a.activeWorkspace}
	}
	return nil
}
