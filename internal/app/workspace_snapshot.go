package app

import "github.com/andyrewlee/amux/internal/data"

func snapshotWorkspaceForSave(ws *data.Workspace) *data.Workspace {
	if ws == nil {
		return nil
	}

	snapshot := *ws
	if ws.OpenTabs != nil {
		snapshot.OpenTabs = make([]data.TabInfo, len(ws.OpenTabs))
		copy(snapshot.OpenTabs, ws.OpenTabs)
	}
	if ws.Env != nil {
		snapshot.Env = make(map[string]string, len(ws.Env))
		for key, value := range ws.Env {
			snapshot.Env[key] = value
		}
	}

	return &snapshot
}
