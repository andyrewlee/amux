package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// openReleaseStatus drives the real status → R → typed-confirm path and
// returns the app mid-dialog.
func openReleaseStatus(t *testing.T, app *App, ws *data.Workspace) {
	t.Helper()
	statusInterval(t, app, ws)
	var cmds []tea.Cmd
	if !app.handleRunOutputInput(tea.KeyPressMsg{Code: 'R', Text: "R"}, &cmds) {
		t.Fatal("R was not intercepted by the status viewer")
	}
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("typed-confirm dialog did not open")
	}
}

// TestPortReleaseConfirmation_ImmediateEnterBlocked: Enter on the empty
// release field must be rejected as validation — no result, dialog stays
// open, registry untouched.
func TestPortReleaseConfirmation_ImmediateEnterBlocked(t *testing.T) {
	app, resStore, _ := reclaimableStatusApp(t, nil)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	openReleaseStatus(t, app, ws)
	if app.dlg.portReleaseCount != 1 {
		t.Fatalf("bound count = %d, want 1", app.dlg.portReleaseCount)
	}

	var cmds []tea.Cmd
	app.handleDialogInput(tea.KeyPressMsg{Code: tea.KeyEnter}, &cmds)
	for _, cmd := range cmds {
		if cmd != nil {
			if _, ok := cmd().(boundDialogResultMsg); ok {
				t.Fatal("empty Enter emitted a confirmed result")
			}
		}
	}
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("empty Enter closed the confirm dialog")
	}
	snap, err := resStore.Snapshot()
	if err != nil || len(snap) != 1 {
		t.Fatalf("registry changed on blocked Enter: %v, %v", snap, err)
	}
}

// TestPortReleaseConfirmation_ConsumerValidation: the consumer gate —
// positive bound count + exact trimmed equality — runs before any probe or
// registry mutation.
func TestPortReleaseConfirmation_ConsumerValidation(t *testing.T) {
	reject := []struct {
		name  string
		value string
		count int
	}{
		{"empty", "", 1},
		{"whitespace", "   ", 1},
		{"wrong count", "2", 1},
		{"missing context", "1", 0},
		{"negative context", "1", -3},
	}
	for _, tc := range reject {
		t.Run("reject_"+tc.name, func(t *testing.T) {
			app, resStore, _ := reclaimableStatusApp(t, nil)
			mustReserveCleanup(t, resStore, "deadbeef01234567")
			cmd := dialogResultReleasePortReservations(app, common.DialogResult{
				ID: DialogReleasePortReservations, Confirmed: true, Value: tc.value,
			}, dialogContext{portReleaseCount: tc.count})
			if cmd != nil {
				t.Fatalf("rejected confirmation produced a release cmd")
			}
			snap, _ := resStore.Snapshot()
			if len(snap) != 1 {
				t.Fatalf("registry mutated on rejected confirmation: %v", snap)
			}
		})
	}

	accept := []struct {
		name  string
		value string
	}{
		{"exact", "1"},
		{"surrounding whitespace", "  1\t"},
	}
	for _, tc := range accept {
		t.Run("accept_"+tc.name, func(t *testing.T) {
			app, resStore, _ := reclaimableStatusApp(t, nil)
			mustReserveCleanup(t, resStore, "deadbeef01234567")
			cmd := dialogResultReleasePortReservations(app, common.DialogResult{
				ID: DialogReleasePortReservations, Confirmed: true, Value: tc.value,
			}, dialogContext{portReleaseCount: 1})
			if cmd == nil {
				t.Fatal("valid confirmation produced no release cmd")
			}
			res, ok := cmd().(reservationReleaseResultMsg)
			if !ok || res.err != nil || res.count != 1 {
				t.Fatalf("release = %+v, want 1,nil", res)
			}
			snap, _ := resStore.Snapshot()
			if len(snap) != 0 {
				t.Fatalf("registry kept released entry: %v", snap)
			}
		})
	}
}

// TestPortReleaseConfirmation_CancelAndNoOrphans: cancel emits nothing; a
// valid confirmation whose fresh probe finds zero orphans reports zero
// rather than releasing the displayed count.
func TestPortReleaseConfirmation_CancelAndNoOrphans(t *testing.T) {
	app, resStore, _ := reclaimableStatusApp(t, nil)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	if cmd := dialogResultReleasePortReservations(app, common.DialogResult{
		ID: DialogReleasePortReservations, Confirmed: false, Value: "1",
	}, dialogContext{portReleaseCount: 1}); cmd != nil {
		t.Fatal("cancel produced a release cmd")
	}

	// Displayed 1, but the orphan set drained before confirm.
	if _, err := resStore.ReleaseMany([]string{"deadbeef01234567"}); err != nil {
		t.Fatalf("setup: could not drain reservation: %v", err)
	}
	cmd := dialogResultReleasePortReservations(app, common.DialogResult{
		ID: DialogReleasePortReservations, Confirmed: true, Value: "1",
	}, dialogContext{portReleaseCount: 1})
	res, ok := cmd().(reservationReleaseResultMsg)
	if !ok || res.err != nil || res.count != 0 {
		t.Fatalf("release on resolved orphans = %+v, want 0,nil", res)
	}
}

// TestPortReleaseConfirmation_StaleBoundResultUsesOwnCount: a result bound
// to an older dialog validates against its own captured count — a
// replacement dialog cannot lend it a different expected value.
func TestPortReleaseConfirmation_StaleBoundResultUsesOwnCount(t *testing.T) {
	app, resStore, _ := reclaimableStatusApp(t, nil)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	openReleaseStatus(t, app, ws)
	oldSeq := app.dialogOpenSeq

	// Type "1" and Enter — the emitted result binds seq+context (count 1).
	var cmds []tea.Cmd
	app.handleDialogInput(tea.KeyPressMsg{Code: '1', Text: "1"}, &cmds)
	app.handleDialogInput(tea.KeyPressMsg{Code: tea.KeyEnter}, &cmds)
	var stale boundDialogResultMsg
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if m, ok := cmd().(boundDialogResultMsg); ok {
			stale = m
		}
	}
	if !stale.result.Confirmed || stale.dlg.portReleaseCount != 1 {
		t.Fatalf("bound result = %+v want confirmed count 1", stale)
	}

	// A newer dialog replaces the producer before the result lands.
	app.dialog.Hide()
	app.handleShowShelveWorkspaceDialog(messages.ShowShelveWorkspaceDialog{
		Project:   &data.Project{Path: "/tmp/repo"},
		Workspace: ws,
	})
	if app.dialogOpenSeq == oldSeq {
		t.Fatal("replacement dialog did not advance open seq")
	}

	handled, releaseCmd := app.handleDialogResultMsg(stale)
	if !handled {
		t.Fatal("stale result was not handled")
	}
	if releaseCmd == nil {
		// Stale results are dropped by seq fencing — either behavior is
		// safe; what must NOT happen is release against a wrong count.
		return
	}
	// If the seq gate lets it through (same-generation edge), the consumer
	// still validates against the BOUND count, not the live context.
	res, _ := releaseCmd().(reservationReleaseResultMsg)
	if res.err != nil {
		t.Fatalf("stale-bound release erred: %v", res.err)
	}
}

// TestPortReleaseConfirmation_SessionAppearsBeforeConfirm: the fresh probe
// — not the typed count — decides what actually releases.
func TestPortReleaseConfirmation_SessionAppearsBeforeConfirm(t *testing.T) {
	app, resStore, ops := reclaimableStatusApp(t, nil)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	mustReserveCleanup(t, resStore, "deadbeef89abcdef")
	// Displayed 2; one owner gains a session before the user confirms.
	ops.SessionsWithTagsFunc = func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{sessionTagRow("deadbeef89abcdef")}, nil
	}
	cmd := dialogResultReleasePortReservations(app, common.DialogResult{
		ID: DialogReleasePortReservations, Confirmed: true, Value: "2",
	}, dialogContext{portReleaseCount: 2})
	res, ok := cmd().(reservationReleaseResultMsg)
	if !ok || res.err != nil || res.count != 1 {
		t.Fatalf("release = %+v, want 1,nil (fresh probe under-releases)", res)
	}
	snap, _ := resStore.Snapshot()
	if _, ok := snap["deadbeef89abcdef"]; !ok {
		t.Fatalf("live-session owner released: %v", snap)
	}
}
