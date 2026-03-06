package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleProjectsLoaded processes the ProjectsLoaded message.
func (a *App) handleProjectsLoaded(msg messages.ProjectsLoaded) []tea.Cmd {
	a.projects = msg.Projects
	a.projectsLoaded = true
	var cmds []tea.Cmd
	if a.dashboard != nil {
		a.dashboard.SetProjects(a.projects)
	}
	cmds = append(cmds, a.rebindActiveSelection()...)
	// Request git status for all workspaces
	cmds = append(cmds, a.scanTmuxActivityNow())
	if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if gcCmd := a.gcStaleTerminalSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if countCmd := a.logSessionCount(); countCmd != nil {
		cmds = append(cmds, countCmd)
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			cmds = append(cmds, a.requestGitStatus(ws.Root))
		}
	}
	return cmds
}

func (a *App) rebindActiveSelection() []tea.Cmd {
	var cmds []tea.Cmd
	if a.activeWorkspace != nil {
		previous := a.activeWorkspace
		wsID := string(a.activeWorkspace.ID())
		ws, project := a.findWorkspaceAndProjectByID(wsID)
		if ws == nil {
			ws, project = a.findWorkspaceAndProjectByCanonicalPaths(previous.Repo, previous.Root)
		}
		if ws == nil {
			a.goHome()
			a.activeProject = nil
			return cmds
		}
		oldID := string(previous.ID())
		newID := string(ws.ID())
		hadPreviousWorkspaceState := false
		if a.center != nil {
			hadPreviousWorkspaceState = a.center.HasWorkspaceState(oldID)
		}
		if oldID != newID {
			a.migrateDirtyWorkspaceID(oldID, newID)
			cmds = append(cmds, a.rebindActiveWorkspaceWatch(previous.Root, ws.Root)...)
			if a.center != nil {
				if cmd := a.center.RebindWorkspaceID(previous, ws); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			if a.sidebarTerminal != nil {
				if cmd := a.sidebarTerminal.RebindWorkspaceID(previous, ws); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		a.activeWorkspace = ws
		a.activeProject = project
		if a.center != nil {
			a.center.SetWorkspace(ws)
			wsIDCurrent := string(ws.ID())
			hasWorkspaceState := a.center.HasWorkspaceState(wsIDCurrent)
			existingTabs, _ := a.center.GetTabsInfoForWorkspace(wsIDCurrent)
			hasLiveWorkspaceTabs := len(existingTabs) > 0
			shouldHydrateTabs := !hasWorkspaceState || hasLiveWorkspaceTabs
			if shouldHydrateTabs && oldID != newID && hadPreviousWorkspaceState {
				shouldHydrateTabs = false
			}
			if shouldHydrateTabs && a.dirtyWorkspaces != nil && a.dirtyWorkspaces[wsIDCurrent] {
				shouldHydrateTabs = false
			}
			if shouldHydrateTabs {
				if cmd := a.center.AddTabsFromWorkspace(ws, ws.OpenTabs); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		if a.sidebar != nil {
			if cmd := a.sidebar.SetWorkspace(ws); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.SetWorkspacePreview(ws)
		}
		return cmds
	}
	if a.activeProject != nil {
		a.activeProject = a.findProjectByPath(a.activeProject.Path)
	}
	return cmds
}

func (a *App) rebindActiveWorkspaceWatch(previousRoot, currentRoot string) []tea.Cmd {
	var cmds []tea.Cmd
	oldRoot := strings.TrimSpace(previousRoot)
	newRoot := strings.TrimSpace(currentRoot)
	if oldRoot == "" || newRoot == "" || oldRoot == newRoot {
		return cmds
	}

	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(oldRoot)
		if err := a.fileWatcher.Watch(newRoot); err != nil {
			logging.Warn("File watcher error: %v", err)
			if errors.Is(err, git.ErrWatchLimit) && a.fileWatcherErr == nil {
				a.fileWatcherErr = err
				if a.toast != nil {
					cmds = append(cmds, a.toast.ShowWarning("File watching disabled (watch limit reached); git status may be stale"))
				}
			}
		}
	}

	if a.gitStatus != nil {
		a.gitStatus.Invalidate(oldRoot)
		a.gitStatus.Invalidate(newRoot)
	}
	if a.dashboard != nil {
		a.dashboard.InvalidateStatus(oldRoot)
		a.dashboard.InvalidateStatus(newRoot)
	}

	return cmds
}

func rootsReferToSameWorkspace(left, right string) bool {
	leftTrimmed := strings.TrimSpace(left)
	rightTrimmed := strings.TrimSpace(right)
	if leftTrimmed == "" || rightTrimmed == "" {
		return false
	}
	if leftTrimmed == rightTrimmed {
		return true
	}
	return canonicalPathForMatch(leftTrimmed) == canonicalPathForMatch(rightTrimmed)
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

func (a *App) findWorkspaceAndProjectByCanonicalPaths(repoPath, rootPath string) (*data.Workspace, *data.Project) {
	targetRepo := canonicalPathForMatch(repoPath)
	targetRoot := canonicalPathForMatch(rootPath)
	if targetRepo == "" && targetRoot == "" {
		return nil, nil
	}
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			ws := &project.Workspaces[j]
			repoCanonical := canonicalPathForMatch(ws.Repo)
			rootCanonical := canonicalPathForMatch(ws.Root)
			if targetRoot != "" && rootCanonical != targetRoot {
				continue
			}
			if targetRepo != "" && repoCanonical != targetRepo {
				continue
			}
			if targetRoot == "" && targetRepo != "" && repoCanonical != targetRepo {
				continue
			}
			return ws, project
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
	return canonicalPathForMatch(path)
}

func canonicalPathForMatch(path string) string {
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
	centerFocusQueuedReattach := false
	a.activeProject = msg.Project
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	if cmd := a.sidebar.SetWorkspace(msg.Workspace); cmd != nil {
		cmds = append(cmds, cmd)
	}
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
	// Mouse-first behavior: if this workspace already has center chat tabs,
	// route keyboard input to the active chat tab immediately.
	if msg.Workspace != nil {
		wsID := string(msg.Workspace.ID())
		centerVisible := a.layout != nil && a.layout.ShowCenter()
		if centerVisible {
			hasCenterTabs := false
			// Existing in-memory tabs are available immediately for workspaces
			// visited in this process (independent of async tmux discovery cmds).
			if tabs, _ := a.center.GetTabsInfoForWorkspace(wsID); len(tabs) > 0 {
				hasCenterTabs = true
			}
			// Also treat persisted tab metadata as a focus signal so this does
			// not depend on synchronous tab hydration timing.
			if !hasCenterTabs && len(msg.Workspace.OpenTabs) > 0 {
				hasCenterTabs = true
			}
			// When no center-tab signal exists, keep the current focus instead of
			// forcing a dashboard/center jump.
			if hasCenterTabs {
				// focusPane(PaneCenter) already performs the reattach attempt;
				// mark it as queued regardless of returned command to avoid
				// coupling deduplication to a nil/non-nil command shape.
				focusCmd := a.focusPane(messages.PaneCenter)
				centerFocusQueuedReattach = true
				if focusCmd != nil {
					cmds = append(cmds, focusCmd)
				}
			}
		}
		if !centerVisible {
			// Keep keyboard routing on the visible pane in dashboard-only layouts.
			if focusCmd := a.focusPane(messages.PaneDashboard); focusCmd != nil {
				cmds = append(cmds, focusCmd)
			}
		}
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	a.syncActiveWorkspacesToDashboard()
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status for sidebar (full mode for line stats)
	if msg.Workspace != nil {
		cmds = append(cmds, a.requestGitStatusFull(msg.Workspace.Root))
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
	// Seamless UX: if restored active tab is detached, auto-reattach on workspace activation.
	if !centerFocusQueuedReattach {
		cmds = append(cmds, a.center.ReattachActiveTabIfDetached())
	}
	cmds = append(cmds, a.enforceAttachedAgentTabLimit()...)
	return cmds
}

// handleCreateWorkspace handles the CreateWorkspace message.
func (a *App) handleCreateWorkspace(msg messages.CreateWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	name := strings.TrimSpace(msg.Name)
	base := msg.Base
	assistant := strings.TrimSpace(msg.Assistant)
	if assistant == "" {
		cmds = append(cmds, func() tea.Msg {
			return messages.WorkspaceCreateFailed{Err: errors.New("assistant is required")}
		})
		return cmds
	}
	if err := validation.ValidateAssistant(assistant); err != nil {
		cmds = append(cmds, func() tea.Msg {
			return messages.WorkspaceCreateFailed{Err: err}
		})
		return cmds
	}
	if !a.isKnownAssistant(assistant) {
		cmds = append(cmds, func() tea.Msg {
			return messages.WorkspaceCreateFailed{Err: fmt.Errorf("unknown assistant: %s", assistant)}
		})
		return cmds
	}
	if msg.Project != nil && name != "" && a.workspaceService != nil {
		pending := a.workspaceService.pendingWorkspace(msg.Project, name, base)
		if pending != nil {
			pending.Assistant = assistant
			a.creatingWorkspaceIDs[string(pending.ID())] = true
			if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	cmds = append(cmds, a.createWorkspace(msg.Project, name, base, assistant))
	return cmds
}

// handleGitStatusResult handles the GitStatusResult message.
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	return cmd
}
