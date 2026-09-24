package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleShowWorkspaceScriptsDialog shows the workspace scripts editor,
// seeded from the workspace's current Scripts/ScriptMode. The dialog renders
// the trust caveat itself: user-entered commands bypass the repo trust gate
// by design (resolveScriptCommand's ws.Scripts fallback), so this editor is
// where a user types their own commands.
func (a *App) handleShowWorkspaceScriptsDialog(msg messages.ShowWorkspaceScriptsDialog) {
	if msg.Workspace == nil {
		return
	}
	a.requestOverlayOpen(func() {
		a.overlays.scriptsWorkspace = msg.Workspace
		a.overlays.scripts = common.NewScriptsDialog(
			msg.Workspace.Scripts.Setup, msg.Workspace.Scripts.Run,
			msg.Workspace.Scripts.Archive, msg.Workspace.Scripts.OnDone,
			msg.Workspace.ScriptMode)
		a.overlays.scripts.SetSize(a.width, a.height)
		a.overlays.scripts.Show()
	})
}

// handleScriptsDialogResult handles the workspace scripts dialog's close. On
// cancel every edit is discarded (same Esc contract as the env dialog). On
// confirm the edited commands + mode persist via WorkspaceStore.SetScripts —
// the same load-fresh-then-save shape SetEnv uses — and the in-memory active
// workspace reflects the change immediately.
func (a *App) handleScriptsDialogResult(res common.ScriptsDialogResult) tea.Cmd {
	ws := a.overlays.scriptsWorkspace
	a.overlays.scriptsWorkspace = nil
	dialog := a.overlays.scripts
	a.overlays.scripts = nil

	if res.Canceled || ws == nil || dialog == nil {
		return nil
	}
	setup, run, archive, onDone, mode := dialog.Values()
	scripts := data.ScriptsConfig{Setup: setup, Run: run, Archive: archive, OnDone: onDone}

	if a.workspaceService == nil {
		return nil
	}
	// The service owns the identity rule — it stores under MetadataID(), the
	// persisted record key; ws.ID() drifts across worktree create/remove.
	if err := a.workspaceService.SetWorkspaceScripts(ws, scripts, mode); err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving workspace scripts"), err, "")
	}
	if a.activeWorkspace != nil && a.activeWorkspace.Root == ws.Root {
		a.activeWorkspace.Scripts = scripts
		a.activeWorkspace.ScriptMode = mode
	}
	return a.toast.ShowSuccess("Updated scripts for " + ws.Name)
}

// handleShowProjectEnvDialog opens the per-project env editor for the
// workspace's repo — the user-owned layer above repo `env` and beneath
// ws.Env. Seeded from the stored project map (filtered like the workspace
// editor so reserved keys can never become rows); the repo path is stashed
// on the App like envDialogWorkspace is for its dialog.
func (a *App) handleShowProjectEnvDialog(msg messages.ShowProjectEnvDialog) {
	if msg.Workspace == nil || a.projectEnvStore == nil {
		return
	}
	a.requestOverlayOpen(func() {
		a.overlays.projectEnvRepo = msg.Workspace.Repo
		a.overlays.projectEnv = common.NewEnvDialog(filterReservedEnv(a.projectEnvStore.ForRepo(msg.Workspace.Repo)))
		a.overlays.projectEnv.SetScope(common.EnvScopeProject)
		a.overlays.projectEnv.SetKeyValidator(envAddKeyValidator)
		a.overlays.projectEnv.SetTitle("Project Environment (" + filepath.Base(msg.Workspace.Repo) + ")")
		a.overlays.projectEnv.SetSize(a.width, a.height)
		a.overlays.projectEnv.Show()
	})
}

// handleProjectEnvDialogResult persists the project map through
// ProjectEnvStore.Set — the same load-fresh-then-write shape as
// SetEnv, so a stale dialog snapshot cannot clobber concurrent edits.
// The change reaches every workspace of the project on its next script
// spawn (the resolver is consulted per-spawn, not snapshotted).
func (a *App) handleProjectEnvDialogResult(res common.EnvDialogResult) tea.Cmd {
	repo := a.overlays.projectEnvRepo
	a.overlays.projectEnvRepo = ""
	dialog := a.overlays.projectEnv
	a.overlays.projectEnv = nil

	if res.Canceled || repo == "" || dialog == nil || a.projectEnvStore == nil {
		return nil
	}
	if err := a.projectEnvStore.Set(repo, filterReservedEnv(dialog.Env())); err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving project environment"), err, "")
	}
	return a.toast.ShowSuccess("Updated project environment for " + filepath.Base(repo))
}
