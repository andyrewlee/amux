package app

import (
	"errors"
	"fmt"
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
	// Drop a stale reload: under rapid/batch deletes multiple LoadProjects
	// goroutines are in flight with no ordering, and an older one (started before
	// a later delete's store.Delete completed) would otherwise overwrite a.projects
	// and resurrect the deleted workspace. Zero-token messages apply (back-compat).
	loadToken := projectsLoadToken(msg.LoadToken)
	if loadToken != 0 && loadToken < a.lifecycle.lastAppliedProjectsLoadToken {
		return nil
	}
	if loadToken > a.lifecycle.lastAppliedProjectsLoadToken {
		a.lifecycle.lastAppliedProjectsLoadToken = loadToken
	}
	loadedIdentities := projectLoadWorkspaceIdentities(msg.Projects)
	msg.Projects = a.filterDeletedWorkspacesFromProjectLoad(msg.Projects, loadToken)
	a.lifecycle.clearDeletedProjectLoadBarriersThrough(loadToken, loadedIdentities)
	a.projects = msg.Projects
	a.projectsLoaded = true
	var cmds []tea.Cmd
	if a.dashboard != nil {
		a.dashboard.SetProjects(a.projects)
	}
	cmds = append(cmds, a.rebindActiveSelectionForLoad(loadToken)...)
	a.lifecycle.clearCreatedProjectLoadBarriersThrough(loadToken, loadedIdentities)
	// Request git status for all workspaces — one batched refresh Cmd emits a
	// single GitStatusBatchResult, so the reload costs one render pass instead
	// of one per workspace.
	cmds = append(cmds, a.scanTmuxActivityNow())
	if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if countCmd := a.logSessionCount(); countCmd != nil {
		cmds = append(cmds, countCmd)
	}
	statusRoots := make([]string, 0)
	seenRoots := make(map[string]bool)
	a.eachWorkspace(func(ws *data.Workspace, _ *data.Project) {
		if ws.Root == "" || seenRoots[ws.Root] {
			return
		}
		seenRoots[ws.Root] = true
		statusRoots = append(statusRoots, ws.Root)
	})
	if statusCmd := a.requestGitStatusBatch(statusRoots); statusCmd != nil {
		cmds = append(cmds, statusCmd)
	}
	return cmds
}

func (a *App) filterDeletedWorkspacesFromProjectLoad(projects []data.Project, loadToken projectsLoadToken) []data.Project {
	for i := range projects {
		project := &projects[i]
		filtered := make([]data.Workspace, 0, len(project.Workspaces))
		for j := range project.Workspaces {
			ws := project.Workspaces[j]
			if a.lifecycle.shouldFilterDeletedWorkspace(string(ws.ID()), ws.Root, loadToken) {
				continue
			}
			filtered = append(filtered, ws)
		}
		project.Workspaces = filtered
	}
	return projects
}

func projectLoadWorkspaceIdentities(projects []data.Project) map[string]bool {
	identities := make(map[string]bool)
	for i := range projects {
		for j := range projects[i].Workspaces {
			ws := projects[i].Workspaces[j]
			if id := string(ws.ID()); id != "" {
				identities[id] = true
			}
			if ws.Root != "" {
				identities[ws.Root] = true
			}
		}
	}
	return identities
}

func (a *App) rebindActiveSelection() []tea.Cmd {
	return a.rebindActiveSelectionForLoad(0)
}

func (a *App) rebindActiveSelectionForLoad(loadToken projectsLoadToken) []tea.Cmd {
	var cmds []tea.Cmd
	if a.activeWorkspace != nil {
		previous := a.activeWorkspace
		wsID := string(a.activeWorkspace.ID())
		ws, project := a.findWorkspaceAndProjectByID(wsID)
		if ws == nil {
			ws, project = a.findWorkspaceAndProjectByCanonicalPaths(previous.Repo, previous.Root)
		}
		if ws == nil {
			if a.lifecycle.isMutatingWorkspace(wsID, previous.Root) {
				return cmds
			}
			if a.lifecycle.shouldRetainCreatedWorkspace(wsID, previous.Root, loadToken) {
				return cmds
			}
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
			if shouldHydrateTabs && a.lifecycle.dirty != nil && a.lifecycle.dirty[wsIDCurrent] {
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

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	centerFocusQueuedReattach := false
	// Capture the previously-active workspace root before it is overwritten so
	// its file watch can be released below: the watcher follows the single
	// active workspace, so the prior root must be unwatched on switch or its OS
	// watch descriptor leaks for the rest of the session.
	var previousActiveRoot string
	if a.activeWorkspace != nil {
		previousActiveRoot = a.activeWorkspace.Root
	}
	a.activeProject = msg.Project
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	if cmd := a.sidebar.SetWorkspace(msg.Workspace); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Bring the run-script indicator in line with the newly active workspace
	// immediately, rather than letting the periodic reconcile show the previous
	// workspace's state for up to a tick. Async: the check shells tmux.
	if cmd := a.requestRunScriptStatus(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	a.sidebarTerminal.SetWorkspacePreview(msg.Workspace)
	// Discover shared tmux tabs first; restore/sync happens below.
	if !a.hasPendingAgentLaunch(msg.Workspace) {
		if discoverCmd := a.discoverWorkspaceTabsFromTmux(msg.Workspace); discoverCmd != nil {
			cmds = append(cmds, discoverCmd)
		}
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
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status for sidebar (full mode for line stats)
	if msg.Workspace != nil {
		cmds = append(cmds, a.requestGitStatusFull(msg.Workspace.Root))
		// Set up file watching for this workspace
		if a.fileWatcher != nil {
			// Release the previously-active workspace's watch before watching the
			// new one. Without this, switching workspaces leaks an OS watch
			// descriptor per distinct workspace until the watch limit is hit and
			// git-status watching is disabled app-wide. Unwatch is idempotent.
			if previousActiveRoot != "" && previousActiveRoot != msg.Workspace.Root {
				a.fileWatcher.Unwatch(previousActiveRoot)
			}
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
	// Re-check the terminal limit too: activation changes which workspace's
	// terminals are exempt, so a workspace that grew over the limit while
	// active becomes evictable only once the user switches away.
	a.enforceAttachedTerminalTabLimit()
	// Start the assistant picked during creation, now that this workspace owns
	// the center pane. The tab this creates re-runs the limit checks above via
	// TabCreated, so it is enforced against the new tab too.
	if cmd := a.launchPendingAgent(msg.Workspace); cmd != nil {
		cmds = append(cmds, cmd)
	}
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
	var pending *data.Workspace
	if msg.Project != nil && name != "" && a.workspaceService != nil {
		pending = a.workspaceService.PendingWorkspace(msg.Project, name, base)
		if pending != nil {
			pending.Assistant = assistant
			if !a.lifecycle.markCreatingWorkspace(string(pending.ID()), pending.Root) {
				cmds = append(cmds, func() tea.Msg {
					return messages.WorkspaceCreateFailed{
						Workspace: pending,
						Err:       fmt.Errorf("workspace %s is already being deleted", pending.Name),
					}
				})
				return cmds
			}
			if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	cmds = append(cmds, wrapLifecycleCmd(a.createWorkspace(msg.Project, name, base, assistant), lifecycleOpCreate, msg.Project, pending))
	return cmds
}
