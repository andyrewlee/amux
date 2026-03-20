package app

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

func (a *App) persistDeferredSandboxSync(source, target *data.Workspace) {
	if source == nil || target == nil {
		return
	}
	lookupIDs := a.pendingSandboxSyncKeys(source, target)
	if a.sandboxManager == nil {
		a.storePendingSandboxSync(lookupIDs, *source, *target)
		return
	}
	if err := a.sandboxManager.PersistPendingSyncTarget(source, target); err != nil {
		logging.Warn("Sandbox deferred sync persistence failed for %s -> %s: %v", source.Root, target.Root, err)
		a.storePendingSandboxSync(lookupIDs, *source, *target)
		return
	}
	a.storePendingSandboxSync(lookupIDs, *target, *target)
}
