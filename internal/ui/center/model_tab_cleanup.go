package center

import (
	"github.com/andyrewlee/amux/internal/data"
)

func (m *Model) CleanupWorkspace(ws *data.Workspace, stampedIDs []string) {
	if ws == nil {
		return
	}
	// Prefer the pre-removal identity set stamped on the message; ws.ID()
	// drifts once the worktree is gone (NormalizePath only resolves existing
	// paths), so the stamped set is what the tabs were actually keyed under.
	// Fall back to both computed forms for messages that predate stamping.
	wsIDs := stampedIDs
	if len(wsIDs) == 0 {
		wsIDs = data.WorkspaceIdentityStrings(ws)
	}

	// Close resources for each tab before removing
	var closing []*Tab
	for _, wsID := range wsIDs {
		for _, tab := range m.tabs.ByWorkspace[wsID] {
			closing = append(closing, tab)
			tab.markClosing()
			m.stopPTYReader(tab)
			tab.mu.Lock()
			if tab.ptyTraceFile != nil {
				_ = tab.ptyTraceFile.Close()
				tab.ptyTraceFile = nil
				tab.ptyTraceClosed = true
			}
			tab.resetPTYStateLocked()
			tab.DiffViewer = nil
			tab.Terminal = nil
			tab.ResetSnapshotCache()
			tab.Workspace = nil
			tab.Running = false
			tab.retireTabInputWriterLocked()
			tab.mu.Unlock()
			tab.markClosed()
		}
		m.tabs.DeleteWorkspace(wsID)
		if m.deletedWorkspaceIDs == nil {
			m.deletedWorkspaceIDs = make(map[string]struct{})
		}
		m.deletedWorkspaceIDs[wsID] = struct{}{}
	}
	m.noteTabsChanged()

	// Also cleanup agents for this workspace — this is what unblocks any
	// in-flight input write, so the retired writers joined below can exit.
	if m.agentManager != nil {
		m.agentManager.CloseWorkspaceAgents(ws, wsIDs)
	}
	for _, tab := range closing {
		tab.joinRetiredInputWriters()
	}
}
