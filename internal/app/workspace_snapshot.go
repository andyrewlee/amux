package app

import "github.com/andyrewlee/amux/internal/data"

func snapshotWorkspaceForSave(ws *data.Workspace) *data.Workspace {
	if ws == nil {
		return nil
	}
	snapshot := ws.Clone()
	return &snapshot
}
