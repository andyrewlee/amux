package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/bulkops"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// newBulkTestApp builds the minimal App the bulk-op driver needs: the
// lifecycle guard, dashboard (for mark clearing + per-row spinner), toast,
// and a workspaceService whose lazy op Cmds are collected but never
// executed — the driver's sequencing is what is under test, not git. The
// driver's seams bind the real per-row handlers, same as production.
func newBulkTestApp() *App {
	a := &App{
		lifecycle:        newWorkspaceLifecycleState(),
		dashboard:        dashboard.New(),
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, nil, ""),
	}
	a.initBulkDriver()
	return a
}

func makeBulkTarget(name string) bulkops.Target {
	ws := &data.Workspace{
		Name:   name,
		Branch: name,
		Repo:   "/repo",
		Root:   "/repo/.amux/workspaces/" + name,
	}
	proj := &data.Project{Name: "repo", Path: "/repo"}
	return bulkops.Target{Project: proj, Workspace: ws, MarkID: string(ws.MetadataID())}
}

// TestFinishWorkspaceShelvedSuppressesToastDuringBulk covers the app-side
// read of the driver: a per-row success toast is suppressed while a
// same-kind batch drains (the drain summary replaces it) but survives a
// foreign-kind batch.
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

	// Arm a real same-kind batch — head in flight on x, y queued.
	targets := []bulkops.Target{makeBulkTarget("x"), makeBulkTarget("y")}
	if cmd := app.bulk.Start(bulkops.Shelve, targets); cmd == nil {
		t.Fatal("expected the batch's first op cmds")
	}
	app.finishWorkspaceShelved(ws, nil)
	if strings.Contains(ansi.Strip(app.toast.View()), "Shelved a") {
		t.Fatal("per-row toast must be suppressed while a batch is active")
	}

	// A foreign-kind batch does NOT suppress: a purge drain must not eat a
	// manual shelve's toast.
	app.toast.Dismiss()
	app.bulk.Abandon()
	if cmd := app.bulk.Start(bulkops.Purge, targets); cmd == nil {
		t.Fatal("expected the purge batch's first op cmds")
	}
	app.finishWorkspaceShelved(ws, nil)
	if !strings.Contains(ansi.Strip(app.toast.View()), "Shelved a") {
		t.Fatal("per-row toast must survive a foreign-kind batch")
	}
}

func TestBulkConfirmBodyCapsList(t *testing.T) {
	targets := make([]bulkops.Target, 0, 12)
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

// TestDialogResultBulkPurgeRejectsWrongCount is the typed-confirm gate:
// any value other than the exact count must not start the drain.
func TestDialogResultBulkPurgeRejectsWrongCount(t *testing.T) {
	for _, value := range []string{"", "1", "3", "two", " 2 x"} {
		app := newBulkTestApp()
		dlg := dialogContext{bulkTargets: []bulkops.Target{makeBulkTarget("a"), makeBulkTarget("b")}}
		cmd := dialogResultBulkPurgeWorkspace(app, common.DialogResult{ID: DialogBulkPurgeWorkspace, Confirmed: true, Value: value}, dlg)
		if app.bulk.Active() {
			t.Fatalf("value %q started the drain — the gate failed", value)
		}
		if cmd == nil {
			t.Fatalf("value %q: expected a cancel-feedback cmd, got nil", value)
		}
	}
}

func TestDialogResultBulkPurgeAcceptsExactCount(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkops.Target{makeBulkTarget("a"), makeBulkTarget("b")}
	dlg := dialogContext{bulkTargets: targets}

	cmd := dialogResultBulkPurgeWorkspace(app, common.DialogResult{ID: DialogBulkPurgeWorkspace, Confirmed: true, Value: "2"}, dlg)
	if cmd == nil {
		t.Fatal("exact count must start the drain")
	}
	if !app.bulk.ActiveFor(bulkops.Purge) {
		t.Fatal("purge batch not started")
	}
	if app.bulk.HeadID() != targets[0].MarkID {
		t.Fatalf("head = %q, want %q", app.bulk.HeadID(), targets[0].MarkID)
	}
}

// TestDialogResultBulkDeleteRejectsWrongCount is the typed-confirm gate:
// any value other than the exact count must not start the drain.
func TestDialogResultBulkDeleteRejectsWrongCount(t *testing.T) {
	for _, value := range []string{"", "1", "3", "two", " 2 x"} {
		app := newBulkTestApp()
		dlg := dialogContext{bulkTargets: []bulkops.Target{makeBulkTarget("a"), makeBulkTarget("b")}}
		cmd := dialogResultBulkDeleteWorkspace(app, common.DialogResult{ID: DialogBulkDeleteWorkspace, Confirmed: true, Value: value}, dlg)
		if app.bulk.Active() {
			t.Fatalf("value %q started the drain — the gate failed", value)
		}
		if cmd == nil {
			t.Fatalf("value %q: expected a cancel-feedback cmd, got nil", value)
		}
	}
}

func TestDialogResultBulkDeleteAcceptsExactCount(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkops.Target{makeBulkTarget("a"), makeBulkTarget("b")}
	dlg := dialogContext{bulkTargets: targets}

	cmd := dialogResultBulkDeleteWorkspace(app, common.DialogResult{ID: DialogBulkDeleteWorkspace, Confirmed: true, Value: "2"}, dlg)
	if cmd == nil {
		t.Fatal("exact count must start the drain")
	}
	if !app.bulk.ActiveFor(bulkops.Delete) {
		t.Fatal("delete batch not started")
	}
	if app.bulk.HeadID() != targets[0].MarkID {
		t.Fatalf("head = %q, want %q", app.bulk.HeadID(), targets[0].MarkID)
	}
}
