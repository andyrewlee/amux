package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/bulkops"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// initBulkDriver binds the bulk driver's seams to this App — the per-row
// ops are the literal lifecycle handlers so a batch drains through the
// same guarded, spinner-backed path a single op runs, and the summary
// plus dashboard-mark clearing land through the real toast/dashboard.
// Called by newAppShell; test fixtures wiring the same fields call it too.
func (a *App) initBulkDriver() {
	a.bulk = bulkops.New(bulkops.Seams{
		Shelve: func(p *data.Project, ws *data.Workspace) []tea.Cmd {
			return a.handleShelveWorkspace(messages.ShelveWorkspace{Project: p, Workspace: ws})
		},
		Restore: func(p *data.Project, ws *data.Workspace) []tea.Cmd {
			return a.handleRestoreWorkspace(messages.RestoreWorkspace{Project: p, Workspace: ws})
		},
		// Live and shelved rows share the delete flow — the per-row
		// handler already tolerates the absent worktree on purge.
		Delete: func(p *data.Project, ws *data.Workspace) []tea.Cmd {
			return a.handleDeleteWorkspace(messages.DeleteWorkspace{Project: p, Workspace: ws})
		},
		ClearMarks: func(ids []string) {
			if a.dashboard != nil {
				a.dashboard.ClearMarks(ids)
			}
		},
		Warn:    func(msg string) tea.Cmd { return a.toast.ShowWarning(msg) },
		Success: func(msg string) tea.Cmd { return a.toast.ShowSuccess(msg) },
	})
}

// resolveBulkTargets re-resolves a queued bulk-open's captured targets
// against the loaded projects at drain time (requestOverlayOpen's contract):
// shelved=false matches project.Workspaces, shelved=true matches
// project.ShelvedWorkspaces. required carries whether the WHOLE set was
// loaded at request time (bulkTargetsRequired evaluated by the handler): a
// fully-loaded set whose membership changed while queued invalidates the open
// — the typed count and row list were computed from the request-time set, so
// presenting anyway would confirm a different operation than the user sees.
// The user re-marks and retries.
func (a *App) resolveBulkTargets(targets []bulkops.Target, shelved, required bool) ([]bulkops.Target, bool) {
	resolved := make([]bulkops.Target, 0, len(targets))
	for _, t := range targets {
		var ws *data.Workspace
		var proj *data.Project
		if shelved {
			ws, proj = a.findShelvedWorkspaceAndProjectByID(string(t.Workspace.ID()))
		} else {
			ws, proj = a.findWorkspaceAndProjectByID(string(t.Workspace.ID()))
		}
		if ws == nil {
			if !required {
				resolved = append(resolved, t)
				continue
			}
			logging.Debug("Dropping stale bulk overlay open: workspace %s no longer loaded", t.Workspace.Name)
			return nil, false
		}
		resolved = append(resolved, bulkops.Target{Project: proj, Workspace: ws, MarkID: t.MarkID})
	}
	return resolved, true
}

// bulkTargetsRequired reports whether every captured target is loaded at
// request time — shelved matching resolveBulkTargets' lookup — and is the
// required flag it expects.
func (a *App) bulkTargetsRequired(targets []bulkops.Target, shelved bool) bool {
	for _, t := range targets {
		var ws *data.Workspace
		if shelved {
			ws, _ = a.findShelvedWorkspaceAndProjectByID(string(t.Workspace.ID()))
		} else {
			ws, _ = a.findWorkspaceAndProjectByID(string(t.Workspace.ID()))
		}
		if ws == nil {
			return false
		}
	}
	return true
}

// handleShowBulkShelveWorkspaceDialog opens the single confirm dialog for
// the marked shelve set — the bulk counterpart of
// handleShowShelveWorkspaceDialog.
func (a *App) handleShowBulkShelveWorkspaceDialog(msg messages.ShowBulkShelveWorkspaceDialog) {
	targets := bulkops.TargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.presentBulkTargetsDialog(targets, false, func(targets []bulkops.Target) {
		a.dlg.bulkTargets = targets
		a.dialog = common.NewConfirmDialog(
			DialogBulkShelveWorkspace,
			fmt.Sprintf("Shelve %d Workspaces", len(targets)),
			bulkConfirmBody(
				fmt.Sprintf("Shelve %d workspaces?", len(targets)),
				targets,
				"Each worktree is removed; branches and settings are kept for restore.",
			),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowBulkRestoreWorkspaceDialog opens the confirm dialog for the
// marked shelved set — the bulk counterpart of a shelved row's Enter.
func (a *App) handleShowBulkRestoreWorkspaceDialog(msg messages.ShowBulkRestoreWorkspaceDialog) {
	targets := bulkops.TargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.presentBulkTargetsDialog(targets, true, func(targets []bulkops.Target) {
		a.dlg.bulkTargets = targets
		a.dialog = common.NewConfirmDialog(
			DialogBulkRestoreWorkspace,
			fmt.Sprintf("Restore %d Workspaces", len(targets)),
			bulkConfirmBody(
				fmt.Sprintf("Restore %d workspaces?", len(targets)),
				targets,
				"Each worktree is recreated from its kept branch and settings.",
			),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowBulkPurgeWorkspaceDialog opens the typed confirmation for the
// marked shelved set. Purge is destructive — branch, metadata, and
// settings go with the workspace — so it requires typing the count rather
// than a reflexive Enter: the input validator rejects anything but the
// exact number, and the result handler re-verifies before the drain
// starts.
func (a *App) handleShowBulkPurgeWorkspaceDialog(msg messages.ShowBulkPurgeWorkspaceDialog) {
	targets := bulkops.TargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.presentBulkTargetsDialog(targets, true, func(targets []bulkops.Target) {
		a.dlg.bulkTargets = targets
		want := strconv.Itoa(len(targets))
		a.dialog = common.NewInputDialog(
			DialogBulkPurgeWorkspace,
			fmt.Sprintf("Purge %d Workspaces", len(targets)),
			"Type "+want+" to confirm",
		)
		a.dialog.SetInputValidate(func(s string) string {
			if s == "" {
				return "" // no nag on empty — the hint is already visible
			}
			if strings.TrimSpace(s) != want {
				return "Type " + want + " to confirm the purge"
			}
			return ""
		})
		a.presentDialog(a.dialog)
	})
}

// handleShowBulkDeleteWorkspaceDialog opens the typed confirmation for the
// marked live set. Deleting a live row removes worktree, branch, metadata,
// and settings — the same op a single delete runs — so it shares purge's
// typed-count gate rather than the shelve/restore plain confirm.
func (a *App) handleShowBulkDeleteWorkspaceDialog(msg messages.ShowBulkDeleteWorkspaceDialog) {
	targets := bulkops.TargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.presentBulkTargetsDialog(targets, false, func(targets []bulkops.Target) {
		a.dlg.bulkTargets = targets
		want := strconv.Itoa(len(targets))
		a.dialog = common.NewInputDialog(
			DialogBulkDeleteWorkspace,
			fmt.Sprintf("Delete %d Workspaces", len(targets)),
			"Type "+want+" to confirm",
		)
		a.dialog.SetInputValidate(func(s string) string {
			if s == "" {
				return "" // no nag on empty — the hint is already visible
			}
			if strings.TrimSpace(s) != want {
				return "Type " + want + " to confirm the delete"
			}
			return ""
		})
		a.presentDialog(a.dialog)
	})
}

// bulkConfirmBody lists the marked workspace names — the bulk version of
// the single op's "what happens" copy. Names are capped so a large fleet
// doesn't overflow the dialog.
func bulkConfirmBody(action string, targets []bulkops.Target, detail string) string {
	const maxListed = 8
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Workspace.Name)
	}
	shown := names
	if len(shown) > maxListed {
		shown = shown[:maxListed]
	}
	body := strings.Join(shown, ", ")
	if extra := len(names) - len(shown); extra > 0 {
		body += fmt.Sprintf(" +%d more", extra)
	}
	return fmt.Sprintf("%s (%s)\n%s", action, body, detail)
}

// dialogResultBulkShelveWorkspace starts the sequential drain on confirm.
func dialogResultBulkShelveWorkspace(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if len(dlg.bulkTargets) == 0 {
		return nil
	}
	return a.bulk.Start(bulkops.Shelve, dlg.bulkTargets)
}

// dialogResultBulkRestoreWorkspace starts the restore drain on confirm.
func dialogResultBulkRestoreWorkspace(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if len(dlg.bulkTargets) == 0 {
		return nil
	}
	return a.bulk.Start(bulkops.Restore, dlg.bulkTargets)
}

// dialogResultBulkPurgeWorkspace re-verifies the typed count before the
// destructive drain starts — the input validator is UX; this check is the
// gate. A wrong value drops the confirm so the drain cannot start.
func dialogResultBulkPurgeWorkspace(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if len(dlg.bulkTargets) == 0 {
		return nil
	}
	if strings.TrimSpace(result.Value) != strconv.Itoa(len(dlg.bulkTargets)) {
		return a.toast.ShowWarning("Purge canceled — the count did not match")
	}
	return a.bulk.Start(bulkops.Purge, dlg.bulkTargets)
}

// dialogResultBulkDeleteWorkspace re-verifies the typed count before the
// destructive live-row drain starts — same gate as bulk purge.
func dialogResultBulkDeleteWorkspace(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if len(dlg.bulkTargets) == 0 {
		return nil
	}
	if strings.TrimSpace(result.Value) != strconv.Itoa(len(dlg.bulkTargets)) {
		return a.toast.ShowWarning("Delete canceled — the count did not match")
	}
	return a.bulk.Start(bulkops.Delete, dlg.bulkTargets)
}
