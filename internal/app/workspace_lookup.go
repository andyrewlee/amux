package app

import "github.com/andyrewlee/amux/internal/data"

func (a *App) findWorkspaceByID(id string) *data.Workspace {
	if id == "" {
		return nil
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			if string(ws.ID()) == id {
				return ws
			}
		}
	}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == id {
		return a.activeWorkspace
	}
	return nil
}
