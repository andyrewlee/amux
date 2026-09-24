package dashboard

import tea "charm.land/bubbletea/v2"

// Temporary shim so the pre-refactor internal/app code compiles while the
// stack lands in order; removed by the app commit.
func (m *Model) SetWorkspaceDeleting(root string, deleting bool) tea.Cmd {
	return m.SetWorkspaceBusy(root, WorkspaceOpDelete, deleting)
}
