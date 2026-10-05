package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func visibleRunOutput() *common.OutputDialog {
	d := common.NewOutputDialog("t", "c")
	d.Show()
	return d
}

// arbiterTestApp is the minimal App for arbiter tests — presentDialog reads
// a.config for keymap hints, so it can't stay nil.
func arbiterTestApp() *App {
	return &App{config: &config.Config{}}
}

// TestOverlayOpenDefersBehindBespokeOverlay is the core regression: a dialog
// open requested while a bespoke overlay is visible must NOT open — today it
// would stack beneath the overlay and eat input invisibly.
func TestOverlayOpenDefersBehindBespokeOverlay(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()

	a.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Workspace: &data.Workspace{Name: "ws-a"},
	})
	if a.dialog != nil {
		t.Fatal("dialog opened beneath a visible bespoke overlay")
	}
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected 1 queued open, got %d", len(a.pendingOverlayOpens))
	}
	if a.dlg.workspace != nil {
		t.Fatal("dlg context bound before the deferred open ran — it must bind at drain time")
	}
}

// TestOverlayOpenDrainsOnClose: when the visible overlay closes, the queued
// open fires and its context binds then (not at enqueue time). The dialog's
// subject must be a loaded workspace — opens re-resolve their target at
// drain, so a registered record is what binds.
func TestOverlayOpenDrainsOnClose(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	ws := &data.Workspace{Name: "ws-a", Repo: "/repo", Root: "/repo/ws-a"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}

	a.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Workspace: ws})
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if a.dialog == nil || !a.dialog.Visible() {
		t.Fatal("queued dialog did not open after the overlay closed")
	}
	if a.dlg.workspace == nil || a.dlg.workspace.Name != "ws-a" {
		t.Fatal("dlg context did not bind the re-resolved workspace at drain")
	}
	if len(a.pendingOverlayOpens) != 0 {
		t.Fatalf("queue not drained: %d remaining", len(a.pendingOverlayOpens))
	}
}

// TestOverlayOpenDrainsViaUpdate pins the end-of-Update drain: closing the
// overlay then processing ANY message releases the queue.
func TestOverlayOpenDrainsViaUpdate(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	a := h.app
	a.overlays.runOutput = visibleRunOutput()
	a.requestOverlayOpen(func() {
		a.dialog = common.NewConfirmDialog(DialogQuit, "q", "q?")
		a.presentDialog(a.dialog)
	})
	if a.dialog != nil {
		t.Fatal("open ran while overlay visible")
	}
	a.overlays.runOutput.Hide()
	if _, _ = a.Update(tea.WindowSizeMsg{Width: 120, Height: 40}); a.dialog == nil {
		t.Fatal("queued open did not drain on the Update after the overlay closed")
	}
}

// TestOverlayOpenFIFO: multiple queued opens run oldest-first, one per
// drain pass (each open makes the next see a visible overlay).
func TestOverlayOpenFIFO(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()

	var order []string
	a.requestOverlayOpen(func() {
		order = append(order, "first")
		a.overlays.env = common.NewEnvDialog(nil)
		a.overlays.env.Show()
	})
	a.requestOverlayOpen(func() { order = append(order, "second") })

	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()
	if len(order) != 1 || order[0] != "first" {
		t.Fatalf("expected only 'first' to run while its overlay shows, got %v", order)
	}
	// The first open's overlay now blocks the second; closing it releases it.
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()
	if len(order) != 2 || order[1] != "second" {
		t.Fatalf("expected 'second' after first overlay closed, got %v", order)
	}
}

// TestOverlayOpenImmediatePath: nothing visible → open runs synchronously
// (preserves existing behavior for all normal opens).
func TestOverlayOpenImmediatePath(t *testing.T) {
	a := arbiterTestApp()
	ran := false
	a.requestOverlayOpen(func() { ran = true })
	if !ran {
		t.Fatal("open did not run immediately with no overlay visible")
	}
	if len(a.pendingOverlayOpens) != 0 {
		t.Fatal("immediate open leaked into the queue")
	}
}

// TestOverlayOpenQueueCap: a wedged overlay must not let the queue grow
// unboundedly — opens past the cap drop.
func TestOverlayOpenQueueCap(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	for i := 0; i < pendingOverlayOpenCap+4; i++ {
		a.requestOverlayOpen(func() {})
	}
	if len(a.pendingOverlayOpens) != pendingOverlayOpenCap {
		t.Fatalf("queue grew past cap: %d", len(a.pendingOverlayOpens))
	}
}

// TestRunOutputRefreshReplacesVisibleViewer: a fetch result for the
// already-visible run viewer replaces content immediately — queueing it
// behind itself would deadlock.
func TestRunOutputRefreshReplacesVisibleViewer(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()

	replaced := false
	a.requestRunOutputOpen(func() {
		a.overlays.runOutput = common.NewOutputDialog("t2", "new")
		a.overlays.runOutput.Show()
		replaced = true
	})
	if !replaced {
		t.Fatal("refresh of the visible run viewer was queued instead of applied")
	}
	if len(a.pendingOverlayOpens) != 0 {
		t.Fatal("visible-viewer refresh leaked into the queue")
	}
}

// TestRunOutputOpenDefersBehindOtherOverlay: the same open behind a
// DIFFERENT visible overlay defers like any other open.
func TestRunOutputOpenDefersBehindOtherOverlay(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.env = common.NewEnvDialog(nil)
	a.overlays.env.Show()

	a.requestRunOutputOpen(func() {
		a.overlays.runOutput = common.NewOutputDialog("t", "c")
		a.overlays.runOutput.Show()
	})
	if a.overlays.runOutput != nil {
		t.Fatal("run output opened beneath the env overlay")
	}
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()
	if a.overlays.runOutput == nil || !a.overlays.runOutput.Visible() {
		t.Fatal("queued run output did not open after env closed")
	}
}

// TestDialogVsDialogStillRejects pins the contract: a synchronous
// Show* while a dialog is already open returns early — it is rejected, not
// queued (queueing is only for dialog-vs-bespoke-overlay).
func TestDialogVsDialogStillRejects(t *testing.T) {
	a := arbiterTestApp()
	live := common.NewConfirmDialog(DialogDeleteWorkspace, "live", "live?")
	live.Show()
	a.dialog = live

	a.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{
		Workspace: &data.Workspace{Name: "ws-b"},
	})
	if a.dialog != live {
		t.Fatal("incoming Show replaced the live dialog")
	}
	if len(a.pendingOverlayOpens) != 0 {
		t.Fatal("dialog-vs-dialog open was queued — it must be rejected, not deferred")
	}
}

// TestRunSessionPickerSupersededAtDrain: a picker open queued behind an
// overlay is superseded by a later R press (token bump) — presenting it would
// show stale rows whose confirm then walks through the current-token fence.
func TestRunSessionPickerSupersededAtDrain(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.env = common.NewEnvDialog(nil)
	a.overlays.env.Show()
	ws := &data.Workspace{Name: "ws-runs"}
	a.overlays.runOutputToken = 5

	a.handleRunSessionsEnumerated(runSessionsEnumeratedMsg{
		token: 5,
		ws:    ws,
		entries: []process.RunSessionEntry{
			{Name: "run", Ordinal: 1, Alive: true},
			{Name: "run-2", Ordinal: 2, Alive: false},
		},
	})
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected the picker open queued, got %d", len(a.pendingOverlayOpens))
	}

	// Second R press before the queue drains.
	a.overlays.runOutputToken = 6
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()

	if a.dialog != nil {
		t.Fatal("superseded picker presented — the drain-time token check must drop it")
	}
	if a.dlg.workspace != nil {
		t.Fatal("superseded open still bound dlg context")
	}
}

// TestRunSessionPickerDrainsWhenCurrent: the queued picker with the live
// token presents with its enumerated rows.
func TestRunSessionPickerDrainsWhenCurrent(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.env = common.NewEnvDialog(nil)
	a.overlays.env.Show()
	ws := &data.Workspace{Name: "ws-runs"}
	a.overlays.runOutputToken = 7

	entries := []process.RunSessionEntry{
		{Name: "run", Ordinal: 1, Alive: true},
		{Name: "run-2", Ordinal: 2, Alive: false},
	}
	a.handleRunSessionsEnumerated(runSessionsEnumeratedMsg{token: 7, ws: ws, entries: entries})
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()

	if a.dialog == nil || !a.dialog.Visible() {
		t.Fatal("current-token picker did not present")
	}
	if len(a.dlg.runSessions) != 2 {
		t.Fatalf("expected 2 run-session rows bound, got %d", len(a.dlg.runSessions))
	}
	if a.dlg.workspace != ws {
		t.Fatal("picker did not bind its workspace")
	}
}

// TestOverlayOpenDropsStaleWorkspaceSubject: a queued delete open whose
// workspace left the loaded projects while queued must not present — the
// confirmation would target a record that no longer exists.
func TestOverlayOpenDropsStaleWorkspaceSubject(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	ws := &data.Workspace{Name: "ws-gone", Repo: "/repo", Root: "/repo/ws-gone"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}

	a.handleShowDeleteWorkspaceDialog(messages.ShowDeleteWorkspaceDialog{Workspace: ws})
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected the open queued, got %d", len(a.pendingOverlayOpens))
	}

	// The workspace is removed while the open sits queued.
	a.projects = nil
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if a.dialog != nil {
		t.Fatal("delete dialog presented for a workspace that no longer exists")
	}
	if a.dlg.workspace != nil {
		t.Fatal("stale open bound dlg context")
	}
}

// TestResolveBulkTargetsRejectsMissingMember: a marked set whose membership
// changed while queued invalidates the whole open — the typed count was
// computed from the request-time set.
func TestResolveBulkTargetsRejectsMissingMember(t *testing.T) {
	a := arbiterTestApp()
	wsLive := &data.Workspace{Name: "ws-live", Repo: "/repo", Root: "/repo/ws-live"}
	wsGone := &data.Workspace{Name: "ws-gone", Repo: "/repo", Root: "/repo/ws-gone"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*wsLive}}}

	targets := []bulkTarget{
		{project: &a.projects[0], workspace: wsLive, markID: string(wsLive.MetadataID())},
		{project: &a.projects[0], workspace: wsGone, markID: string(wsGone.MetadataID())},
	}
	if _, ok := a.resolveBulkTargets(targets, false, true); ok {
		t.Fatal("expected the set to reject when one member left the loaded projects")
	}
	// A target that was never loaded at request time resolves leniently —
	// the fence is drift, not existence.
	if resolved, ok := a.resolveBulkTargets(targets, false, false); !ok || len(resolved) != 2 {
		t.Fatalf("expected unattached targets to resolve leniently, got %v ok=%v", resolved, ok)
	}

	resolved, ok := a.resolveBulkTargets(targets[:1], false, true)
	if !ok {
		t.Fatal("expected the intact set to resolve")
	}
	if len(resolved) != 1 || resolved[0].workspace.Name != "ws-live" {
		t.Fatalf("expected the live target re-resolved, got %+v", resolved)
	}
}
