package app

import "github.com/andyrewlee/amux/internal/data"

// eachWorkspace visits every workspace across all loaded projects in order,
// passing a pointer to the live workspace (so callers may mutate it in place)
// and its owning project. It centralizes the project/workspace traversal; the
// per-call match or filter logic stays in fn. Use eachWorkspaceUntil when a
// caller needs to stop on the first match.
func (a *App) eachWorkspace(fn func(ws *data.Workspace, project *data.Project)) {
	a.eachWorkspaceUntil(func(ws *data.Workspace, project *data.Project) bool {
		fn(ws, project)
		return false
	})
}

// eachWorkspaceUntil visits workspaces across all loaded projects in order and
// stops as soon as fn returns true, returning true in that case (false if no
// visit stopped early). It preserves the early-exit semantics of the hand-
// written lookup loops it replaces; callers capture any matched workspace via
// the closure.
func (a *App) eachWorkspaceUntil(fn func(ws *data.Workspace, project *data.Project) bool) bool {
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			ws := &project.Workspaces[j]
			if fn(ws, project) {
				return true
			}
		}
	}
	return false
}

func (a *App) findWorkspaceByID(id string) *data.Workspace {
	if id == "" {
		return nil
	}
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == id {
		return a.activeWorkspace
	}
	var found *data.Workspace
	a.eachWorkspaceUntil(func(ws *data.Workspace, _ *data.Project) bool {
		if string(ws.ID()) == id {
			found = ws
			return true
		}
		return false
	})
	return found
}
