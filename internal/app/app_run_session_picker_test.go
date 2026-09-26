package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// seedMultiRunHost builds the concurrent-run fixture the picker tests share:
// a workspace with three tagged sessions — base alive, -2 dead/exit-7, -3
// dead/exit-0 — so enumeration rows exercise every rendered state.
func seedMultiRunHost(t *testing.T, h *Harness, ws *data.Workspace) (*stubRunSessionHost, string) {
	t.Helper()
	base := "amux-ws-" + string(ws.ID()) + "-run"
	host := &stubRunSessionHost{
		tails: map[string]string{
			base:        "base live tail",
			base + "-2": "second run tail",
			base + "-3": "third run tail",
		},
		alive: map[string]bool{base: true, base + "-2": false, base + "-3": false},
		exits: map[string]int{base + "-2": 7, base + "-3": 0},
	}
	runner := process.NewScriptRunner(6200, 10)
	runner.SetRunHost(host)
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")
	return host, base
}

// enumerateRunSessions runs the R-open's enumerate stage and returns the
// delivered message (the picker's row set).
func enumerateRunSessions(t *testing.T, h *Harness, ws *data.Workspace) runSessionsEnumeratedMsg {
	t.Helper()
	cmd := h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the open handler to return an enumerate cmd")
	}
	enum, ok := cmd().(runSessionsEnumeratedMsg)
	if !ok {
		t.Fatalf("open cmd emitted %T, want runSessionsEnumeratedMsg", cmd())
	}
	return enum
}

func TestRunSessionPicker_MultiSessionOpensChooser(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	_, base := seedMultiRunHost(t, h, ws)

	enum := enumerateRunSessions(t, h, ws)
	if len(enum.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(enum.entries))
	}
	if cmd := h.app.handleRunSessionsEnumerated(enum); cmd != nil {
		t.Fatal("the picker path must not also run a tail fetch")
	}
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("multi-session R must open the run-session picker")
	}
	if len(h.app.dlg.runSessions) != 3 {
		t.Fatalf("dlg.runSessions = %d, want 3", len(h.app.dlg.runSessions))
	}
	if h.app.dlg.runSessions[1].Name != base+"-2" || h.app.dlg.runSessions[2].Name != base+"-3" {
		t.Fatalf("entries not in numeric order: %+v", h.app.dlg.runSessions)
	}
	view := h.app.dialog.View()
	for _, want := range []string{"#1 " + base + " — running", "#2 " + base + "-2 — exited 7", "#3 " + base + "-3 — exited 0"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker row %q missing, got:\n%s", want, view)
		}
	}
	if h.app.overlays.runOutput != nil {
		t.Fatal("picker open must not also open the output viewer")
	}
}

// pickerConfirm drives the dialog like a keystroke would: pump Update for
// enter, then route the emitted result through the bound-result path so the
// arbitration guard (seq binding) stays in the loop.
func pickerConfirm(t *testing.T, h *Harness, navKeys ...string) {
	t.Helper()
	for _, k := range navKeys {
		d, _ := h.app.dialog.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		h.app.dialog = d
	}
	d, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	h.app.dialog = d
	if cmd == nil {
		t.Fatal("enter on the picker produced no result cmd")
	}
	res, ok := cmd().(common.DialogResult)
	if !ok {
		t.Fatalf("picker cmd emitted %T, want DialogResult", cmd())
	}
	if !res.Confirmed {
		t.Fatal("enter produced an unconfirmed result")
	}
	handled, apply := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    h.app.dlg,
		result: res,
	})
	if !handled {
		t.Fatal("bound picker result was not routed")
	}
	if apply == nil {
		t.Fatal("confirmed pick should yield the tail fetch cmd")
	}
	opened, ok := apply().(runOutputOpenedMsg)
	if !ok {
		t.Fatalf("fetch emitted %T, want runOutputOpenedMsg", apply())
	}
	h.app.handleRunOutputOpened(opened)
}

func TestRunSessionPicker_ConfirmOpensPinnedViewer(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	_, base := seedMultiRunHost(t, h, ws)

	enum := enumerateRunSessions(t, h, ws)
	h.app.handleRunSessionsEnumerated(enum)

	// Navigate j → #2, j → #3, then confirm: the viewer must open pinned to
	// run-3's tail, not the newest/base session.
	pickerConfirm(t, h, "j", "j")
	if h.app.overlays.runOutput == nil || !h.app.overlays.runOutput.Visible() {
		t.Fatal("confirmed pick did not open the output viewer")
	}
	if h.app.overlays.runOutputSession != base+"-3" {
		t.Fatalf("pinned session = %q, want %q", h.app.overlays.runOutputSession, base+"-3")
	}
	view := h.app.overlays.runOutput.View()
	if !strings.Contains(view, "third run tail") {
		t.Fatalf("viewer shows the wrong session's tail, got:\n%s", view)
	}
	if !strings.Contains(view, "#3") {
		t.Fatalf("viewer title missing the session ordinal, got:\n%s", view)
	}
}

func TestRunSessionPicker_EscCancelsWithoutViewer(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	seedMultiRunHost(t, h, ws)

	enum := enumerateRunSessions(t, h, ws)
	h.app.handleRunSessionsEnumerated(enum)
	d, cmd := h.app.dialog.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	h.app.dialog = d
	if cmd == nil {
		t.Fatal("esc produced no result cmd")
	}
	res, ok := cmd().(common.DialogResult)
	if !ok {
		t.Fatalf("esc emitted %T, want DialogResult", cmd())
	}
	handled, apply := h.app.handleDialogResultMsg(boundDialogResultMsg{
		seq:    h.app.dialogOpenSeq,
		dlg:    h.app.dlg,
		result: res,
	})
	if !handled {
		t.Fatal("cancel result was not routed")
	}
	if apply != nil {
		t.Fatal("canceled picker must not start a tail fetch")
	}
	if h.app.overlays.runOutput != nil {
		t.Fatal("canceled picker opened the output viewer")
	}
}

// TestRunSessionPicker_AttachTargetsPinnedSession pins the viewer→attach
// handoff: `a` while viewing -3 must attach -3, not the newest alive session
// — the defect where attach silently followed a different run.
func TestRunSessionPicker_AttachTargetsPinnedSession(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	host, base := seedMultiRunHost(t, h, ws)
	// Make -3 alive too so an unpinned attach would resolve -3 (newest alive),
	// while the viewed session is -2 — the distinguisher fixture.
	host.alive[base+"-2"] = true
	host.tails[base+"-3"] = "third live tail"

	enum := enumerateRunSessions(t, h, ws)
	h.app.handleRunSessionsEnumerated(enum)
	pickerConfirm(t, h, "j") // pick #2

	var cmds []tea.Cmd
	if !h.app.handleRunOutputInput(tea.KeyPressMsg{Code: 'a', Text: "a"}, &cmds) {
		t.Fatal("attachable run-output dialog must consume 'a'")
	}
	if len(cmds) != 1 {
		t.Fatalf("expected one attach cmd, got %d", len(cmds))
	}
	msg, ok := cmds[0]().(runAttachTargetMsg)
	if !ok {
		t.Fatalf("attach cmd emitted %T, want runAttachTargetMsg", cmds[0]())
	}
	if !msg.ok || msg.name != base+"-2" {
		t.Fatalf("target = %q,%v; want pinned %q,true", msg.name, msg.ok, base+"-2")
	}
}

// TestRunSessionPicker_AttachDeadSessionReports pins the died-between-enum-
// and-attach path: the picked session exited, so `a` surfaces "no live
// session" rather than attaching a dead pane.
func TestRunSessionPicker_AttachDeadSessionReports(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	seedMultiRunHost(t, h, ws)

	enum := enumerateRunSessions(t, h, ws)
	h.app.handleRunSessionsEnumerated(enum)
	pickerConfirm(t, h, "j") // pick #2 — dead exit 7

	var cmds []tea.Cmd
	if !h.app.handleRunOutputInput(tea.KeyPressMsg{Code: 'a', Text: "a"}, &cmds) {
		t.Fatal("attach key must still be consumed for a dead pinned session")
	}
	msg, ok := cmds[0]().(runAttachTargetMsg)
	if !ok {
		t.Fatalf("attach cmd emitted %T, want runAttachTargetMsg", cmds[0]())
	}
	if msg.ok {
		t.Fatal("attach resolved a dead session as live")
	}
	if cmd := h.app.handleRunAttachTarget(msg); cmd == nil {
		t.Fatal("dead attach target should surface a toast")
	}
	if !strings.Contains(h.app.toast.View(), "No live run session") {
		t.Fatalf("expected 'No live run session' toast, got %q", h.app.toast.View())
	}
}

// TestRunSessionPicker_VanishedSelection covers the pick→delete race: the
// chosen session disappeared before the tail fetch, so the open reports
// "no output" instead of showing a blank viewer.
func TestRunSessionPicker_VanishedSelection(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	host, base := seedMultiRunHost(t, h, ws)

	enum := enumerateRunSessions(t, h, ws)
	h.app.handleRunSessionsEnumerated(enum)

	// Kill -3 after enumeration but before the pick lands.
	delete(host.alive, base+"-3")
	delete(host.tails, base+"-3")

	pickerConfirm(t, h, "j", "j") // pick #3 — now gone
	if h.app.overlays.runOutput != nil {
		t.Fatal("vanished session opened a viewer")
	}
	if !strings.Contains(h.app.toast.View(), "No run output") {
		t.Fatalf("expected 'No run output' toast, got %q", h.app.toast.View())
	}
}
