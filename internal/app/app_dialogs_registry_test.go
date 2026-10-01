package app

import (
	"os"
	"regexp"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestAppDialogIDsCoverConstants guards the single-source-of-truth contract:
// every Dialog* constant declared in app_core.go (plus the agent-picker and
// run-session-picker runtime IDs) must be a member of appDialogIDs. If a new
// dialog constant is added without registering it, its DialogResult would
// silently misroute to a component instead of handleDialogResult — this test
// fails first.
//
// The constant set is derived mechanically by scanning app_core.go (precedent:
// internal/process/noslog_test.go) so adding a Dialog* const without a
// registry entry fails this test — a hand-maintained list here already missed
// DialogBulkDeleteWorkspace once.
func TestAppDialogIDsCoverConstants(t *testing.T) {
	raw, err := os.ReadFile("app_core.go")
	if err != nil {
		t.Fatalf("read app_core.go for Dialog* enumeration: %v", err)
	}
	declRe := regexp.MustCompile(`(?m)^\s*(Dialog\w+)\s*=\s*"([^"]+)"`)
	declared := declRe.FindAllStringSubmatch(string(raw), -1)
	if len(declared) < 10 {
		t.Fatalf("scanned only %d Dialog* constants from app_core.go; the regex probably stopped matching", len(declared))
	}

	for _, m := range declared {
		name, id := m[1], m[2]
		if !isAppDialogID(id) {
			t.Errorf("%s = %q is not registered in appDialogIDs; "+
				"its DialogResult would misroute to a component", name, id)
		}
		if appDialogHandlers[id] == nil {
			t.Errorf("%s = %q routes as App-level but has no registered handler", name, id)
		}
	}

	// Runtime IDs registered without a Dialog* constant — emitted by widgets,
	// not declared in app_core.go. Keep this list explicit and short.
	runtimeIDs := []string{
		common.AgentPickerDialogID,
		common.RunSessionPickerDialogID,
	}
	for _, id := range runtimeIDs {
		if appDialogHandlers[id] == nil {
			t.Errorf("runtime dialog ID %q has no registered handler", id)
		}
	}

	// Reverse direction: every registered handler must trace back to a
	// declared constant or a listed runtime ID — catches a handler keyed by a
	// stale/deleted constant string.
	known := make(map[string]string, len(declared)+len(runtimeIDs))
	for _, m := range declared {
		known[m[2]] = m[1]
	}
	for _, id := range runtimeIDs {
		known[id] = "runtime ID"
	}
	for id := range appDialogHandlers {
		if _, ok := known[id]; !ok {
			t.Errorf("registered dialog ID %q has no Dialog* constant in app_core.go and is not a listed runtime ID", id)
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
