package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestAppDialogIDsCoverConstants guards the single-source-of-truth contract:
// every Dialog* constant declared in app_core.go (plus the agent-picker runtime
// ID) must be a member of appDialogIDs. If a new dialog constant is added
// without registering it here, its DialogResult would silently misroute to a
// component instead of handleDialogResult — this test fails first.
func TestAppDialogIDsCoverConstants(t *testing.T) {
	constants := []string{
		DialogAddProject,
		DialogCreateWorkspace,
		DialogDeleteWorkspace,
		DialogRenameWorkspace,
		DialogCommitWorkspace,
		DialogMergeWorkspace,
		DialogMergeConflict,
		DialogTrustScripts,
		DialogShelveWorkspace,
		DialogRemoveProject,
		common.AgentPickerDialogID,
		DialogQuit,
		DialogCleanupTmux,
	}
	for _, id := range constants {
		if !isAppDialogID(id) {
			t.Errorf("dialog ID %q is not registered in appDialogIDs; "+
				"its DialogResult would misroute to a component", id)
		}
		if appDialogHandlers[id] == nil {
			t.Errorf("dialog ID %q routes as App-level but has no registered handler", id)
		}
	}
}

// TestAppDialogIDsNoDuplicates ensures the list form has no duplicate entries
// (which would mask a missing distinct ID and inflate the set's apparent size).
func TestAppDialogIDsNoDuplicates(t *testing.T) {
	seen := make(map[string]struct{}, len(appDialogIDList))
	for _, id := range appDialogIDList {
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate dialog ID %q in appDialogIDList", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != len(appDialogIDs) {
		t.Errorf("appDialogIDList has %d unique IDs but appDialogIDs has %d",
			len(seen), len(appDialogIDs))
	}
}

// TestUnknownDialogIDIsNotAppLevel sanity-checks the negative case: an ID that
// is not in the registry must route away from the App.
func TestUnknownDialogIDIsNotAppLevel(t *testing.T) {
	if isAppDialogID("definitely-not-a-real-dialog") {
		t.Fatal("unexpected: unknown ID reported as an App-level dialog")
	}
}

// TestRegisteredDialogDispatchesWithoutOtherEdits proves the ≤2-touches
// contract: a dialog ID plus ONE appDialogHandlers entry is sufficient for
// routing + result dispatch — no switch case, no allow-list edit. The fake
// dialog's "keypress" is the DialogResult its widget emits; the registry entry
// alone must carry it to the handler with the captured dialogContext.
func TestRegisteredDialogDispatchesWithoutOtherEdits(t *testing.T) {
	const fakeID = "registry-proof-dialog"
	var gotResult common.DialogResult
	var gotDlg dialogContext
	called := 0
	appDialogHandlers[fakeID] = func(_ *App, r common.DialogResult, d dialogContext) tea.Cmd {
		called++
		gotResult = r
		gotDlg = d
		return func() tea.Msg { return "handled" }
	}
	t.Cleanup(func() { delete(appDialogHandlers, fakeID) })
	appDialogIDs[fakeID] = struct{}{}
	t.Cleanup(func() { delete(appDialogIDs, fakeID) })

	ws := &data.Workspace{Name: "ws-x"}
	a := &App{dlg: dialogContext{workspace: ws}}
	consumed, cmd := a.handleDialogResultMsg(common.DialogResult{
		ID:        fakeID,
		Confirmed: true,
		Value:     "v",
	})
	if !consumed {
		t.Fatal("registered dialog result was not consumed at App level")
	}
	if cmd == nil {
		t.Fatal("handler cmd was dropped")
	}
	if called != 1 {
		t.Fatalf("handler ran %d times, want 1", called)
	}
	if gotResult.Value != "v" || gotDlg.workspace != ws {
		t.Fatalf("handler got result=%+v dlg=%+v, want value v + workspace ws-x", gotResult, gotDlg)
	}
	if msg := cmd(); msg != "handled" {
		t.Fatalf("cmd returned %v, want handler's msg", msg)
	}
}
