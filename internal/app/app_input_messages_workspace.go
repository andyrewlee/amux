package app

import (
	"errors"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleProjectsLoaded processes the ProjectsLoaded message.
func (a *App) handleProjectsLoaded(msg messages.ProjectsLoaded) []tea.Cmd {
	a.projects = msg.Projects
	a.projectsLoaded = true
	if a.dashboard != nil {
		a.dashboard.SetProjects(a.projects)
	}
	a.rebindActiveSelection()
	// Request git status for all workspaces
	var cmds []tea.Cmd
	cmds = append(cmds, a.scanTmuxActivityNow())
	if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if gcCmd := a.gcStaleTerminalSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			cmds = append(cmds, a.requestGitStatus(ws.Root))
		}
	}
	return cmds
}

func (a *App) rebindActiveSelection() {
	if a.activeWorkspace != nil {
		wsID := string(a.activeWorkspace.ID())
		ws, project := a.findWorkspaceAndProjectByID(wsID)
		if ws == nil {
			a.goHome()
			a.activeProject = nil
			return
		}
		a.activeWorkspace = ws
		a.activeProject = project
		if a.center != nil {
			a.center.SetWorkspace(ws)
		}
		if a.sidebar != nil {
			a.sidebar.SetWorkspace(ws)
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.SetWorkspacePreview(ws)
		}
		return
	}
	if a.activeProject != nil {
		a.activeProject = a.findProjectByPath(a.activeProject.Path)
	}
}

func (a *App) findWorkspaceAndProjectByID(id string) (*data.Workspace, *data.Project) {
	if id == "" {
		return nil, nil
	}
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			ws := &project.Workspaces[j]
			if string(ws.ID()) == id {
				return ws, project
			}
		}
	}
	return nil, nil
}

func (a *App) findProjectByPath(path string) *data.Project {
	if path == "" {
		return nil
	}
	targetCanonical := canonicalProjectPathForMatch(path)
	for i := range a.projects {
		project := &a.projects[i]
		if project.Path == path {
			return project
		}
		if targetCanonical == "" {
			continue
		}
		if canonicalProjectPathForMatch(project.Path) == targetCanonical {
			return project
		}
	}
	return nil
}

func canonicalProjectPathForMatch(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return filepath.Clean(cleaned)
}

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeProject = msg.Project
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	a.sidebarTerminal.SetWorkspacePreview(msg.Workspace)
	// Discover shared tmux tabs first; restore/sync happens below.
	if discoverCmd := a.discoverWorkspaceTabsFromTmux(msg.Workspace); discoverCmd != nil {
		cmds = append(cmds, discoverCmd)
	}
	if discoverTermCmd := a.discoverSidebarTerminalsFromTmux(msg.Workspace); discoverTermCmd != nil {
		cmds = append(cmds, discoverTermCmd)
	}
	if syncCmd := a.syncWorkspaceTabsFromTmux(msg.Workspace); syncCmd != nil {
		cmds = append(cmds, syncCmd)
	}
	if restoreCmd := a.center.RestoreTabsFromWorkspace(msg.Workspace); restoreCmd != nil {
		cmds = append(cmds, restoreCmd)
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	a.syncActiveWorkspacesToDashboard()
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status for sidebar
	if msg.Workspace != nil {
		cmds = append(cmds, a.requestGitStatus(msg.Workspace.Root))
		// Set up file watching for this workspace
		if a.fileWatcher != nil {
			if err := a.fileWatcher.Watch(msg.Workspace.Root); err != nil {
				logging.Warn("File watcher error: %v", err)
				if errors.Is(err, git.ErrWatchLimit) && a.fileWatcherErr == nil {
					a.fileWatcherErr = err
					cmds = append(cmds, a.toast.ShowWarning("File watching disabled (watch limit reached); git status may be stale"))
				}
			}
		}
	}
	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	return cmds
}

// handleCreateWorkspace handles the CreateWorkspace message.
func (a *App) handleCreateWorkspace(msg messages.CreateWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	name := strings.TrimSpace(msg.Name)
	if msg.Project != nil && name != "" && a.workspaceService != nil {
		pending, _ := a.workspaceService.pendingWorkspace(msg.Project, name, msg.Base)
		if pending != nil {
			a.creatingWorkspaceIDs[string(pending.ID())] = true
			if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	cmds = append(cmds, a.createWorkspace(msg.Project, name, msg.Base))
	return cmds
}

// handleGitStatusResult handles the GitStatusResult message.
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if a.activeWorkspace != nil && msg.Root == a.activeWorkspace.Root {
		a.sidebar.SetGitStatus(msg.Status)
	}
	return cmd
}
