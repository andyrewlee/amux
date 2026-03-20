package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

func (a *App) rebindInactiveSandboxWorkspaces(previousProjects []data.Project) []tea.Cmd {
	var cmds []tea.Cmd
	for i := range previousProjects {
		for j := range previousProjects[i].Workspaces {
			previous := &previousProjects[i].Workspaces[j]
			if a.isActiveWorkspaceReference(previous) {
				continue
			}
			current, _ := a.findWorkspaceAndProjectByID(string(previous.ID()))
			if current == nil {
				current, _ = a.findWorkspaceAndProjectByCanonicalPaths(previous.Repo, previous.Root)
			}
			if current == nil {
				current, _ = a.findWorkspaceAndProjectByLogicalIdentity(previous)
			}
			if current == nil || !workspaceBindingChanged(previous, current) {
				continue
			}
			oldID := strings.TrimSpace(string(previous.ID()))
			newID := strings.TrimSpace(string(current.ID()))
			if oldID != "" && newID != "" && oldID != newID {
				a.rememberReboundWorkspaceID(oldID, newID)
				a.retargetPendingSandboxSyncs(oldID, current)
				if cmd := a.rebindWorkspaceTmuxSessions(oldID, newID); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			switch {
			case a.shouldSyncFromSandbox(previous, current):
				if a.sandboxManager == nil {
					continue
				}
				if err := a.sandboxManager.PersistPendingSyncTarget(previous, current); err != nil {
					logging.Warn("Inactive workspace sandbox sync-down target persistence failed for %s -> %s: %v", previous.Root, current.Root, err)
					a.trackPendingSandboxSync(previous, current)
				}
			case data.NormalizeRuntime(previous.Runtime) == data.RuntimeCloudSandbox &&
				data.NormalizeRuntime(current.Runtime) == data.RuntimeCloudSandbox:
				if a.sandboxManager == nil {
					continue
				}
				a.sandboxManager.rebindWorkspace(previous, current)
			}
		}
	}
	return cmds
}

func (a *App) isActiveWorkspaceReference(workspace *data.Workspace) bool {
	if workspace == nil || a.activeWorkspace == nil {
		return false
	}
	if workspace.ID() == a.activeWorkspace.ID() {
		return true
	}
	return canonicalPathForMatch(workspace.Repo) == canonicalPathForMatch(a.activeWorkspace.Repo) &&
		canonicalPathForMatch(workspace.Root) == canonicalPathForMatch(a.activeWorkspace.Root)
}

func workspaceBindingChanged(previous, current *data.Workspace) bool {
	if previous == nil || current == nil {
		return false
	}
	return previous.ID() != current.ID() ||
		previous.Repo != current.Repo ||
		previous.Root != current.Root ||
		data.NormalizeRuntime(previous.Runtime) != data.NormalizeRuntime(current.Runtime)
}

func (a *App) findWorkspaceAndProjectByLogicalIdentity(previous *data.Workspace) (*data.Workspace, *data.Project) {
	if previous == nil {
		return nil, nil
	}
	var matchedWorkspace *data.Workspace
	var matchedProject *data.Project
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			workspace := &project.Workspaces[j]
			if strings.TrimSpace(workspace.Name) != strings.TrimSpace(previous.Name) {
				continue
			}
			if strings.TrimSpace(workspace.Branch) != strings.TrimSpace(previous.Branch) {
				continue
			}
			if strings.TrimSpace(workspace.Base) != strings.TrimSpace(previous.Base) {
				continue
			}
			if matchedWorkspace != nil {
				return nil, nil
			}
			matchedWorkspace = workspace
			matchedProject = project
		}
	}
	return matchedWorkspace, matchedProject
}
