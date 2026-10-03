package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// overlayState groups the bespoke (non-registry) modal dialogs and their
// per-dialog context. Each dialog's context field pairs it with the subject
// it was opened for — the app-side scratch the dialog's own model cannot
// hold because internal/ui/common does not import internal/data (mirroring
// dlg.workspace's role for the generic Dialog, but tracked per slot since a
// bespoke dialog is not routed through a.dlg).
type overlayState struct {
	// settings is the Settings dialog; settingsSession is the generation
	// counter invalidating stale theme previews from a superseded instance.
	settings        *common.SettingsDialog
	settingsSession int
	// Theme persistence state for settings exits: the last theme known to be
	// persisted, whether the live theme diverges from it, and the theme that
	// was active when the dialog opened (restored on Esc).
	settingsPersistedTheme common.ThemeID
	settingsThemeDirty     bool
	settingsThemeOriginal  common.ThemeID
	// App-lifetime save obligations for already confirmed settings values:
	// a failed save keeps its section pending so the next confirmation
	// retries it even when that dialog changed nothing. Update owns them —
	// they are never reset on show, dialog replacement, or Esc.
	settingsUIPersistPending         bool
	settingsAssistantsPersistPending bool
	// env edits one workspace's env map; envWorkspace is read back in
	// handleEnvDialogResult. Its results carry EnvScopeWorkspace.
	env          *common.EnvDialog
	envWorkspace *data.Workspace
	// projectEnv edits the per-project env map (user-owned layer above repo
	// env, beneath ws.Env); projectEnvRepo is the normalized repo path it
	// persists to. Its results carry EnvScopeProject, so dispatch routes on
	// the emitted scope — never on which pointer is non-nil.
	projectEnv     *common.EnvDialog
	projectEnvRepo string
	// scripts edits a workspace's user-entered setup/run/archive commands;
	// scriptsWorkspace is read back in handleScriptsDialogResult.
	scripts          *common.ScriptsDialog
	scriptsWorkspace *data.Workspace
	// runOutput is the read-only viewer for captured script/status output
	// (last pane tail; live or post-exit). runOutputWorkspace is the refresh
	// loop's fetch target and runOutputToken invalidates in-flight refreshes
	// on close/reopen the way settingsSession does for stale theme previews.
	runOutput          *common.OutputDialog
	runOutputWorkspace *data.Workspace
	// runOutputSession pins the viewer to one tmux run session — the
	// picker's selection, or the sole session on the single-run fast path.
	// Refresh ticks and the `a` attach target it, not "the newest", so the
	// user keeps reading the run they picked.
	runOutputSession string
	runOutputToken   int
	// runOutputAttachable marks the live-run flavor of the shared OutputDialog
	// (workspace-status and script-transcript viewers leave it false): only
	// that flavor answers the `a` attach key.
	runOutputAttachable bool
	// runOutputReleaseCount marks the workspace-status flavor: the number of
	// orphaned port reservations it reported. >0 answers the `R` typed-confirm
	// release intercept; 0 disables it for every other flavor.
	runOutputReleaseCount int
}

// overlayInputSlot feeds one message into a bespoke overlay and reports
// whether the overlay consumed it.
type overlayInputSlot func(msg tea.Msg, cmds *[]tea.Cmd) bool

// overlaySlot adapts a pointer-typed dialog field (get/set access to the App
// field) into an overlayInputSlot. consumePaste matches handleOverlayInput's
// contract: registry dialog and file picker treat paste as input, and the
// bespoke editors consume it too now that their text fields accept paste —
// otherwise the same content would fall through to the focused terminal.
func overlaySlot[T interface {
	Visible() bool
	Update(tea.Msg) (T, tea.Cmd)
}](get func() T, set func(T), consumePaste bool) overlayInputSlot {
	return func(msg tea.Msg, cmds *[]tea.Cmd) bool {
		updated, consumed := handleOverlayInput(get(), msg, cmds, consumePaste)
		set(updated)
		return consumed
	}
}

// overlayChain is the pre-switch input chain for modal overlays, in consume
// order — the first live overlay eats the keystroke. Order is load-bearing
// and matches the historical sequential check order exactly: registry dialog
// (result-binding wrapper) → file picker → settings → workspace env →
// project env → scripts → run-output viewer.
func (a *App) overlayChain() []overlayInputSlot {
	return []overlayInputSlot{
		a.handleDialogInput,
		a.handleFilePickerInput,
		overlaySlot(func() *common.SettingsDialog { return a.overlays.settings },
			func(v *common.SettingsDialog) { a.overlays.settings = v }, true),
		overlaySlot(func() *common.EnvDialog { return a.overlays.env },
			func(v *common.EnvDialog) { a.overlays.env = v }, true),
		overlaySlot(func() *common.EnvDialog { return a.overlays.projectEnv },
			func(v *common.EnvDialog) { a.overlays.projectEnv = v }, true),
		overlaySlot(func() *common.ScriptsDialog { return a.overlays.scripts },
			func(v *common.ScriptsDialog) { a.overlays.scripts = v }, true),
		a.handleRunOutputInput,
	}
}

// handleRunOutputInput is the runOutput overlay slot with one app-level key
// intercept: `a` attaches an interactive tab to the viewer's pinned run
// session — but only for the live-run flavor (runOutputAttachable). Every
// other key and the other OutputDialog flavors pass through to the dialog
// unchanged.
func (a *App) handleRunOutputInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	// While the search query field is editing, `a`/`R` are literal query
	// text — the dialog owns all input, so the intercepts are disabled.
	if a.overlays.runOutputAttachable && a.overlays.runOutput != nil && a.overlays.runOutput.Visible() && !a.overlays.runOutput.Editing() {
		if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "a" {
			*cmds = append(*cmds, a.attachRunViewerCmd(a.overlays.runOutputWorkspace, a.overlays.runOutputSession))
			return true
		}
	}
	// Workspace-status flavor: `R` swaps the viewer for the typed-confirm
	// release dialog. Overlay arbitration forbids stacking a dialog over the
	// viewer, so the viewer closes first — cancel returns to the dashboard,
	// confirm re-derives the orphan set fresh before releasing.
	if a.overlays.runOutputReleaseCount > 0 && a.overlays.runOutput != nil && a.overlays.runOutput.Visible() && !a.overlays.runOutput.Editing() {
		if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "R" {
			a.openReservationReleaseDialog(a.overlays.runOutputReleaseCount)
			return true
		}
	}
	// Query-edit mode owns paste (pasted text is query input); browse mode
	// leaves it to the shared chain.
	updated, consumed := handleOverlayInput(a.overlays.runOutput, msg, cmds,
		a.overlays.runOutput != nil && a.overlays.runOutput.Editing())
	a.overlays.runOutput = updated
	return consumed
}
