package app

import (
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
)

// Shutdown releases resources that may outlive the Bubble Tea program.
func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		if a.center != nil {
			a.center.Close()
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.CloseAll()
		}
		if a.sandboxManager != nil {
			a.sandboxManager.CancelLaunchPollers()
			if err := a.sandboxManager.SyncAllToLocal(); err != nil {
				logging.Warn("Sandbox sync-down during shutdown failed: %v", err)
			}
		}
		if a.supervisor != nil {
			a.supervisor.Stop()
		}
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Close()
		}
		if a.stateWatcher != nil {
			_ = a.stateWatcher.Close()
		}
		if a.workspaceService != nil {
			a.workspaceService.StopAll()
		}
		perf.Flush("shutdown")
	})
}
