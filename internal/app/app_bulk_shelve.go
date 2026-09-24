package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// bulkOpKind identifies which per-row lifecycle handler a bulk batch
// drains through.
type bulkOpKind int

const (
	bulkOpShelve bulkOpKind = iota
	bulkOpRestore
	bulkOpPurge
)

// verb is the summary word for the finished batch ("Shelved 3 of 4").
func (k bulkOpKind) verb() string {
	switch k {
	case bulkOpRestore:
		return "Restored"
	case bulkOpPurge:
		return "Purged"
	default:
		return "Shelved"
	}
}

// bulkTarget is one confirmed bulk-op item — the project travels with the
// workspace because dashboard marks can span projects.
type bulkTarget struct {
	project   *data.Project
	workspace *data.Workspace
	markID    string // MetadataID — used to clear the dashboard mark at drain
}

// bulkOpState is the sequential bulk-op driver shared by shelve, restore,
// and purge. The queue drains one workspace at a time through the kind's
// literal per-row handler — the same guarded, spinner-backed op a single
// workspace runs — so worktree add/removals never run in parallel. headID
// is the in-flight op's MetadataID: the matching completion message
// advances the queue, while a foreign completion (a manual op
// interleaving) is ignored and cannot corrupt the accounting.
type bulkOpState struct {
	kind      bulkOpKind
	queue     []bulkTarget
	markIDs   []string // every target's mark — cleared on the dashboard at drain
	headID    string
	total     int
	succeeded int
	failed    int
}

// active reports whether any batch is in progress.
func (b bulkOpState) active() bool { return b.total > 0 }

// activeFor reports whether a batch of the given kind is draining — used
// to suppress that op's per-row toasts in favor of the batch summary at
// drain without eating toasts from an unrelated kind.
func (b bulkOpState) activeFor(kind bulkOpKind) bool {
	return b.active() && b.kind == kind
}

// bulkTargetsFromItems converts the message's item list to driver targets,
// dropping entries that can't run the per-row op anyway.
func bulkTargetsFromItems(items []messages.BulkWorkspaceItem) []bulkTarget {
	targets := make([]bulkTarget, 0, len(items))
	for _, it := range items {
		if it.Project == nil || it.Workspace == nil {
			continue
		}
		targets = append(targets, bulkTarget{
			project:   it.Project,
			workspace: it.Workspace,
			markID:    string(it.Workspace.MetadataID()),
		})
	}
	return targets
}

// handleShowBulkShelveWorkspaceDialog opens the single confirm dialog for
// the marked shelve set — the bulk counterpart of
// handleShowShelveWorkspaceDialog.
func (a *App) handleShowBulkShelveWorkspaceDialog(msg messages.ShowBulkShelveWorkspaceDialog) {
	targets := bulkTargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
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
	targets := bulkTargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
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
	targets := bulkTargetsFromItems(msg.Items)
	if len(targets) == 0 {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
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

// bulkConfirmBody lists the marked workspace names — the bulk version of
// the single op's "what happens" copy. Names are capped so a large fleet
// doesn't overflow the dialog.
func bulkConfirmBody(action string, targets []bulkTarget, detail string) string {
	const maxListed = 8
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.workspace.Name)
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
	return a.startBulkOp(bulkOpShelve, dlg.bulkTargets)
}

// dialogResultBulkRestoreWorkspace starts the restore drain on confirm.
func dialogResultBulkRestoreWorkspace(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if len(dlg.bulkTargets) == 0 {
		return nil
	}
	return a.startBulkOp(bulkOpRestore, dlg.bulkTargets)
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
	return a.startBulkOp(bulkOpPurge, dlg.bulkTargets)
}

// startBulkOp initializes the batch and kicks the first op. A second
// batch while one drains is rejected, not queued — overwriting the state
// would orphan the in-flight op's headID and run two drains in parallel.
func (a *App) startBulkOp(kind bulkOpKind, targets []bulkTarget) tea.Cmd {
	if a.bulk.active() {
		logging.Warn("Bulk op rejected: a batch is already draining")
		return a.toast.ShowWarning("A bulk operation is already running")
	}
	markIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		markIDs = append(markIDs, t.markID)
	}
	a.bulk = bulkOpState{kind: kind, queue: targets, markIDs: markIDs, total: len(targets)}
	return a.advanceBulk()
}

// advanceBulk pops the next target and drives it through the kind's
// literal per-row handler. A guard rejection returns no cmds — that
// target is counted failed and skipped immediately rather than stalling
// the drain. An empty queue means the batch is done: summarize and clear
// marks.
func (a *App) advanceBulk() tea.Cmd {
	b := &a.bulk
	for len(b.queue) > 0 {
		next := b.queue[0]
		b.queue = b.queue[1:]
		b.headID = string(next.workspace.MetadataID())
		var cmds []tea.Cmd
		switch b.kind {
		case bulkOpRestore:
			cmds = a.handleRestoreWorkspace(messages.RestoreWorkspace{
				Project:   next.project,
				Workspace: next.workspace,
			})
		case bulkOpPurge:
			cmds = a.handleDeleteWorkspace(messages.DeleteWorkspace{
				Project:   next.project,
				Workspace: next.workspace,
			})
		default: // bulkOpShelve
			cmds = a.handleShelveWorkspace(messages.ShelveWorkspace{
				Project:   next.project,
				Workspace: next.workspace,
			})
		}
		if len(cmds) == 0 {
			// Rejected by the lifecycle guard (already in another phase)
			// — count and move on; no completion will arrive for it.
			b.headID = ""
			b.failed++
			continue
		}
		return common.SafeBatch(cmds...)
	}
	return a.finishBulk()
}

// bulkFinished records the in-flight op's completion and advances. The ws
// must match the queue head — a completion for any other workspace (a
// manual op interleaving with the batch) belongs to that flow and is left
// to its own accounting.
func (a *App) bulkFinished(ws *data.Workspace, ok bool) tea.Cmd {
	b := &a.bulk
	if b.headID == "" || ws == nil || string(ws.MetadataID()) != b.headID {
		return nil
	}
	b.headID = ""
	if ok {
		b.succeeded++
	} else {
		b.failed++
	}
	return a.advanceBulk()
}

// finishBulk emits the batch summary and clears the batch members'
// dashboard marks — the rows they pointed at are shelved, restored, or
// purged (or failed) now.
func (a *App) finishBulk() tea.Cmd {
	b := a.bulk
	if b.total == 0 {
		return nil
	}
	a.bulk = bulkOpState{}
	if a.dashboard != nil {
		a.dashboard.ClearMarks(b.markIDs)
	}
	verb := b.kind.verb()
	if b.failed > 0 {
		return a.toast.ShowWarning(fmt.Sprintf("%s %d of %d — %d failed", verb, b.succeeded, b.total, b.failed))
	}
	return a.toast.ShowSuccess(fmt.Sprintf("%s %d of %d", verb, b.succeeded, b.total))
}
