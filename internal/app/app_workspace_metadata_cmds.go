package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Async workspace-metadata writes. The store transactions underneath take a
// flock + read-modify-write + fsync, so a contended workspace lock (a second
// amux instance mid-transaction) or a slow filesystem would stall every input,
// tick, and frame for the lock's duration if they ran inline on Update — the
// same reason the tmux/status reads moved off-loop. Each write runs as a
// tea.Cmd carrying a typed result; the result handler applies the in-memory
// reflect and surfaces errors exactly where the inline version did.

// renameWorkspaceResultMsg reports the completed RenameWorkspace transaction.
type renameWorkspaceResultMsg struct {
	workspace *data.Workspace
	name      string
	err       error
}

// workspaceScriptsSavedMsg reports the completed SetWorkspaceScripts
// transaction, carrying the persisted values for the in-memory reflect.
type workspaceScriptsSavedMsg struct {
	workspace *data.Workspace
	scripts   data.ScriptsConfig
	mode      string
	err       error
}

// workspaceEnvSavedMsg reports the completed SetWorkspaceEnv transaction.
type workspaceEnvSavedMsg struct {
	workspace *data.Workspace
	env       map[string]string
	err       error
}

// projectEnvSavedMsg reports the completed ProjectEnvStore.Set transaction.
// There is no in-memory reflect: consumers read the store per script spawn.
type projectEnvSavedMsg struct {
	repo string
	err  error
}

// renameWorkspaceAsync runs the rename transaction off-loop. The workspace is
// cloned at dispatch so the store reads a snapshot, never live app state.
func (a *App) renameWorkspaceAsync(ws *data.Workspace, name string) tea.Cmd {
	if a.workspaceService == nil || ws == nil {
		return nil
	}
	svc := a.workspaceService
	snap := cloneForCmd(ws)
	return func() tea.Msg {
		return renameWorkspaceResultMsg{workspace: snap, name: name, err: svc.RenameWorkspace(snap, name)}
	}
}

// saveWorkspaceScriptsAsync runs the scripts transaction off-loop.
func (a *App) saveWorkspaceScriptsAsync(ws *data.Workspace, scripts data.ScriptsConfig, mode string) tea.Cmd {
	if a.workspaceService == nil || ws == nil {
		return nil
	}
	svc := a.workspaceService
	snap := cloneForCmd(ws)
	return func() tea.Msg {
		return workspaceScriptsSavedMsg{workspace: snap, scripts: scripts, mode: mode, err: svc.SetWorkspaceScripts(snap, scripts, mode)}
	}
}

// saveWorkspaceEnvAsync runs the env transaction off-loop.
func (a *App) saveWorkspaceEnvAsync(ws *data.Workspace, env map[string]string) tea.Cmd {
	if a.workspaceService == nil || ws == nil {
		return nil
	}
	svc := a.workspaceService
	snap := cloneForCmd(ws)
	return func() tea.Msg {
		return workspaceEnvSavedMsg{workspace: snap, env: env, err: svc.SetWorkspaceEnv(snap, env)}
	}
}

// saveProjectEnvAsync runs the project-env transaction off-loop.
func (a *App) saveProjectEnvAsync(repo string, env map[string]string) tea.Cmd {
	if a.projectEnvStore == nil || repo == "" {
		return nil
	}
	store := a.projectEnvStore
	return func() tea.Msg {
		return projectEnvSavedMsg{repo: repo, err: store.Set(repo, env)}
	}
}

// handleRenameWorkspaceResult applies the confirmed rename: the in-memory
// active workspace reflects the new label immediately so the header updates
// without waiting for the async reload, then the reload picks up the store.
func (a *App) handleRenameWorkspaceResult(msg renameWorkspaceResultMsg) tea.Cmd {
	if msg.err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "renaming workspace"), msg.err, "")
	}
	var cmds []tea.Cmd
	if a.activeWorkspace != nil && msg.workspace != nil && a.activeWorkspace.Root == msg.workspace.Root {
		a.activeWorkspace.Name = msg.name
	}
	if cmd := a.toast.ShowSuccess("Renamed workspace to " + msg.name); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.loadProjects())
	return common.SafeBatch(cmds...)
}

// handleWorkspaceScriptsSaved applies the confirmed scripts write to the
// in-memory active workspace, mirroring the pre-async reflect.
func (a *App) handleWorkspaceScriptsSaved(msg workspaceScriptsSavedMsg) tea.Cmd {
	if msg.err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving workspace scripts"), msg.err, "")
	}
	if a.activeWorkspace != nil && msg.workspace != nil && a.activeWorkspace.Root == msg.workspace.Root {
		a.activeWorkspace.Scripts = msg.scripts
		a.activeWorkspace.ScriptMode = msg.mode
	}
	name := ""
	if msg.workspace != nil {
		name = msg.workspace.Name
	}
	return a.toast.ShowSuccess("Updated scripts for " + name)
}

// handleWorkspaceEnvSaved applies the confirmed env write to the in-memory
// active workspace so the app's view does not go stale until the next reload.
func (a *App) handleWorkspaceEnvSaved(msg workspaceEnvSavedMsg) tea.Cmd {
	if msg.err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving workspace environment"), msg.err, "")
	}
	if a.activeWorkspace != nil && msg.workspace != nil && a.activeWorkspace.Root == msg.workspace.Root {
		a.activeWorkspace.Env = msg.env
	}
	name := ""
	if msg.workspace != nil {
		name = msg.workspace.Name
	}
	return a.toast.ShowSuccess("Updated environment for " + name)
}

// handleProjectEnvSaved surfaces the confirmed project-env write. Consumers
// resolve the store per spawn, so there is no app-side mirror to update.
func (a *App) handleProjectEnvSaved(msg projectEnvSavedMsg) tea.Cmd {
	if msg.err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving project environment"), msg.err, "")
	}
	return a.toast.ShowSuccess("Updated project environment for " + filepath.Base(msg.repo))
}
