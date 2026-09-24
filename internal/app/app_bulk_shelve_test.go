package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// newBulkTestApp builds the minimal App the bulk-op driver needs: the
// lifecycle guard, dashboard (for mark clearing + per-row spinner), toast,
// and a workspaceService whose lazy op Cmds are collected but never
// executed — the driver's sequencing is what is under test, not git.
func newBulkTestApp() *App {
	return &App{
		lifecycle:        newWorkspaceLifecycleState(),
		dashboard:        dashboard.New(),
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, nil, ""),
	}
}

func makeBulkTarget(name string) bulkTarget {
	ws := &data.Workspace{
		Name:   name,
		Branch: name,
		Repo:   "/repo",
		Root:   "/repo/.amux/workspaces/" + name,
	}
	proj := &data.Project{Name: "repo", Path: "/repo"}
	return bulkTarget{project: proj, workspace: ws, markID: string(ws.MetadataID())}
}

// toMsg converts a test target into the message form the dashboard emits.
func (t bulkTarget) toMsg() messages.BulkWorkspaceItem {
	return messages.BulkWorkspaceItem{Project: t.project, Workspace: t.workspace}
}

func TestBulkShelveStartsFirstOpOnly(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b"), makeBulkTarget("c")}

	cmd := app.startBulkOp(bulkOpShelve, targets)
	if cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if app.bulk.headID != targets[0].markID {
		t.Fatalf("head = %q, want %q", app.bulk.headID, targets[0].markID)
	}
	if len(app.bulk.queue) != 2 {
		t.Fatalf("queue = %d, want 2", len(app.bulk.queue))
	}
	// Only the head workspace holds the lifecycle in-flight mark.
	if !app.isWorkspaceMutationInFlight(string(targets[0].workspace.ID())) {
		t.Fatal("head workspace not marked in-flight")
	}
	if app.isWorkspaceMutationInFlight(string(targets[1].workspace.ID())) {
		t.Fatal("queued workspace marked in-flight before its turn — not sequential")
	}
}

func TestBulkShelveAdvancesSequentiallyAndDrains(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.dashboard.MarkWorkspaceIDs([]string{targets[0].markID, targets[1].markID})

	app.startBulkOp(bulkOpShelve, targets)

	// ws a completes → b becomes head.
	if cmd := app.bulkFinished(targets[0].workspace, true); cmd == nil {
		t.Fatal("expected next op's cmds after first completion")
	}
	if app.bulk.headID != targets[1].markID {
		t.Fatalf("head after first completion = %q, want %q", app.bulk.headID, targets[1].markID)
	}
	if app.bulk.succeeded != 1 {
		t.Fatalf("succeeded = %d, want 1", app.bulk.succeeded)
	}

	// ws b completes → queue empty → summary toast + marks cleared + state reset.
	if cmd := app.bulkFinished(targets[1].workspace, true); cmd == nil {
		t.Fatal("expected summary toast cmd at drain")
	}
	if app.bulk.active() {
		t.Fatal("batch state not reset after drain")
	}
	if app.dashboard.MarkedCount() != 0 {
		t.Fatalf("marks not cleared at drain: %d remain", app.dashboard.MarkedCount())
	}
	view := ansi.Strip(app.toast.View())
	if !strings.Contains(view, "Shelved 2 of 2") {
		t.Fatalf("summary toast = %q, want 'Shelved 2 of 2'", view)
	}
}

func TestBulkShelveGuardRejectionSkipsAndCounts(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b"), makeBulkTarget("c")}

	// b is already in another lifecycle phase — the per-row guard will
	// reject it, so the drain must count it failed and move on.
	app.markWorkspaceMutationInFlight(targets[1].workspace, true)

	app.startBulkOp(bulkOpShelve, targets)
	// Finish a → advance tries b (rejected → failed++, skipped) → c is head.
	app.bulkFinished(targets[0].workspace, true)
	if app.bulk.failed != 1 {
		t.Fatalf("failed = %d, want 1 (b rejected)", app.bulk.failed)
	}
	if app.bulk.headID != targets[2].markID {
		t.Fatalf("head after rejected item = %q, want %q (c)", app.bulk.headID, targets[2].markID)
	}

	app.bulkFinished(targets[2].workspace, true)
	view := ansi.Strip(app.toast.View())
	if !strings.Contains(view, "Shelved 2 of 3") || !strings.Contains(view, "1 failed") {
		t.Fatalf("summary toast = %q, want 'Shelved 2 of 3 — 1 failed'", view)
	}
}

func TestBulkShelveForeignCompletionIgnored(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.startBulkOp(bulkOpShelve, targets)

	// A manual shelve completing for a workspace that is not the queue
	// head must not advance or count toward the batch.
	foreign := makeBulkTarget("manual")
	if cmd := app.bulkFinished(foreign.workspace, true); cmd != nil {
		t.Fatal("foreign completion produced a cmd — it must be ignored")
	}
	if app.bulk.headID != targets[0].markID {
		t.Fatalf("foreign completion moved head to %q", app.bulk.headID)
	}
	if app.bulk.succeeded != 0 || app.bulk.failed != 0 {
		t.Fatalf("foreign completion corrupted counters: %d/%d", app.bulk.succeeded, app.bulk.failed)
	}
}

func TestFinishWorkspaceShelvedSuppressesToastDuringBulk(t *testing.T) {
	app := newBulkTestApp()
	ws := &data.Workspace{Name: "a", Branch: "a", Repo: "/repo", Root: "/repo/.amux/workspaces/a"}

	// Outside a batch the per-row success toast fires. (The returned cmds
	// include the dismiss tick — not executed: it would sleep the test and
	// expire the very toast under assertion.)
	app.finishWorkspaceShelved(ws, nil)
	if !strings.Contains(ansi.Strip(app.toast.View()), "Shelved a") {
		t.Fatal("single shelve should still toast")
	}
	app.toast.Dismiss()

	// Inside a same-kind batch the toast is suppressed — the drain summary
	// replaces it.
	app.bulk = bulkOpState{kind: bulkOpShelve, total: 2}
	app.finishWorkspaceShelved(ws, nil)
	if strings.Contains(ansi.Strip(app.toast.View()), "Shelved a") {
		t.Fatal("per-row toast must be suppressed while a batch is active")
	}

	// A foreign-kind batch does NOT suppress: a purge drain must not eat a
	// manual shelve's toast.
	app.toast.Dismiss()
	app.bulk = bulkOpState{kind: bulkOpPurge, total: 2}
	app.finishWorkspaceShelved(ws, nil)
	if !strings.Contains(ansi.Strip(app.toast.View()), "Shelved a") {
		t.Fatal("per-row toast must survive a foreign-kind batch")
	}
}

func TestBulkConfirmBodyCapsList(t *testing.T) {
	targets := make([]bulkTarget, 0, 12)
	for _, name := range []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8", "w9", "w10", "w11", "w12"} {
		targets = append(targets, makeBulkTarget(name))
	}
	body := bulkConfirmBody("Shelve 12 workspaces?", targets, "detail")
	if !strings.Contains(body, "Shelve 12 workspaces") {
		t.Fatalf("body missing count: %q", body)
	}
	if !strings.Contains(body, "+4 more") {
		t.Fatalf("body should cap the name list: %q", body)
	}
	if strings.Contains(body, "w9") {
		t.Fatalf("body should list at most 8 names: %q", body)
	}
}

// TestStartBulkOp_RejectsReentry proves a second confirmed batch while one
// drains is rejected — overwriting the state would orphan the in-flight
// head's completion and run two worktree ops in parallel.
func TestStartBulkOp_RejectsReentry(t *testing.T) {
	app := newBulkTestApp()
	first := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	if cmd := app.startBulkOp(bulkOpShelve, first); cmd == nil {
		t.Fatal("first batch must start")
	}
	headID, total := app.bulk.headID, app.bulk.total
	queueLen := len(app.bulk.queue)

	cmd := app.startBulkOp(bulkOpShelve, []bulkTarget{makeBulkTarget("x"), makeBulkTarget("y")})
	if cmd == nil {
		t.Fatal("rejection should still return a toast cmd")
	}
	if app.bulk.total != total || app.bulk.headID != headID || len(app.bulk.queue) != queueLen {
		t.Fatalf("re-entry overwrote the draining batch: %+v", app.bulk)
	}
	// A stale foreign completion still cannot corrupt the draining batch.
	if cmd := app.bulkFinished(makeBulkTarget("x").workspace, true); cmd != nil {
		t.Fatal("foreign completion must not advance the batch")
	}
	if app.bulk.headID != headID {
		t.Fatal("foreign completion cleared the in-flight head")
	}
}

func TestBulkTargetsFromItemsDropsNil(t *testing.T) {
	targets := bulkTargetsFromItems([]messages.BulkWorkspaceItem{
		{Project: nil, Workspace: &data.Workspace{Name: "x"}},
		makeBulkTarget("y").toMsg(),
	})
	if len(targets) != 1 || targets[0].workspace.Name != "y" {
		t.Fatalf("nil-project item must be dropped: %+v", targets)
	}
}

// TestBulkRestoreDrainsSequentially runs the restore kind through the same
// drain: the per-row handler is handleRestoreWorkspace, completions are
// WorkspaceRestored-shaped, and the summary uses the restore verb.
func TestBulkRestoreDrainsSequentially(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.dashboard.MarkWorkspaceIDs([]string{targets[0].markID, targets[1].markID})

	if cmd := app.startBulkOp(bulkOpRestore, targets); cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if app.bulk.kind != bulkOpRestore {
		t.Fatalf("kind = %v, want restore", app.bulk.kind)
	}
	// Sequential: only the head holds the in-flight mark.
	if !app.isWorkspaceMutationInFlight(string(targets[0].workspace.ID())) {
		t.Fatal("head workspace not marked in-flight")
	}
	if app.isWorkspaceMutationInFlight(string(targets[1].workspace.ID())) {
		t.Fatal("queued workspace marked in-flight before its turn")
	}

	if cmd := app.bulkFinished(targets[0].workspace, true); cmd == nil {
		t.Fatal("expected next op's cmds")
	}
	if cmd := app.bulkFinished(targets[1].workspace, true); cmd == nil {
		t.Fatal("expected summary toast at drain")
	}
	if app.bulk.active() {
		t.Fatal("batch not reset after drain")
	}
	if app.dashboard.MarkedCount() != 0 {
		t.Fatal("marks not cleared at drain")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "Restored 2 of 2") {
		t.Fatalf("summary toast = %q, want 'Restored 2 of 2'", view)
	}
}

// TestBulkPurgeDrainsThroughDeleteHandler proves the purge kind drives
// handleDeleteWorkspace: the head carries the delete op's spinner/mutation
// marks and the summary uses the purge verb.
func TestBulkPurgeDrainsThroughDeleteHandler(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.dashboard.MarkWorkspaceIDs([]string{targets[0].markID, targets[1].markID})

	if cmd := app.startBulkOp(bulkOpPurge, targets); cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if !app.isWorkspaceMutationInFlight(string(targets[0].workspace.ID())) {
		t.Fatal("head workspace not marked in-flight")
	}
	app.bulkFinished(targets[0].workspace, true)
	app.bulkFinished(targets[1].workspace, true)
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "Purged 2 of 2") {
		t.Fatalf("summary toast = %q, want 'Purged 2 of 2'", view)
	}
	if app.dashboard.MarkedCount() != 0 {
		t.Fatal("marks not cleared at drain")
	}
}

// TestBulkPurgeGuardRejectionCounts proves a shelved row in another
// lifecycle phase is skipped+counted, not stalled, mid-purge.
func TestBulkPurgeGuardRejectionCounts(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.markWorkspaceMutationInFlight(targets[0].workspace, true)

	app.startBulkOp(bulkOpPurge, targets)
	// a rejected → skipped, b becomes head.
	if app.bulk.failed != 1 || app.bulk.headID != targets[1].markID {
		t.Fatalf("rejected head should count+skip: %+v", app.bulk)
	}
}

// TestDialogResultBulkPurgeRejectsWrongCount is the typed-confirm gate:
// any value other than the exact count must not start the drain.
func TestDialogResultBulkPurgeRejectsWrongCount(t *testing.T) {
	for _, value := range []string{"", "1", "3", "two", " 2 x"} {
		app := newBulkTestApp()
		dlg := dialogContext{bulkTargets: []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}}
		cmd := dialogResultBulkPurgeWorkspace(app, common.DialogResult{ID: DialogBulkPurgeWorkspace, Confirmed: true, Value: value}, dlg)
		if app.bulk.active() {
			t.Fatalf("value %q started the drain — the gate failed", value)
		}
		if cmd == nil {
			t.Fatalf("value %q: expected a cancel-feedback cmd, got nil", value)
		}
	}
}

func TestDialogResultBulkPurgeAcceptsExactCount(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	dlg := dialogContext{bulkTargets: targets}

	cmd := dialogResultBulkPurgeWorkspace(app, common.DialogResult{ID: DialogBulkPurgeWorkspace, Confirmed: true, Value: "2"}, dlg)
	if cmd == nil {
		t.Fatal("exact count must start the drain")
	}
	if !app.bulk.active() || app.bulk.kind != bulkOpPurge {
		t.Fatalf("purge batch not started: %+v", app.bulk)
	}
	if app.bulk.headID != targets[0].markID {
		t.Fatalf("head = %q, want %q", app.bulk.headID, targets[0].markID)
	}
}

// TestBulkFinishedForeignKindIgnored proves a completion for a workspace
// outside the batch cannot advance a purge drain mid-flight.
func TestBulkFinishedForeignKindIgnored(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}
	app.startBulkOp(bulkOpPurge, targets)

	foreign := makeBulkTarget("other")
	if cmd := app.bulkFinished(foreign.workspace, true); cmd != nil {
		t.Fatal("foreign completion must be ignored")
	}
	if app.bulk.headID != targets[0].markID || app.bulk.succeeded != 0 {
		t.Fatalf("foreign completion corrupted the batch: %+v", app.bulk)
	}
}
