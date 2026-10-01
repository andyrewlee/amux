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

// App-level search wiring: the runOutput overlay slot owns two behaviors
// around the dialog's query-edit mode — the app-level `a` attach intercept
// must be suppressed while editing (the key is literal query text), and
// paste must be consumed by the chain while editing but fall through to the
// terminal while browsing. Search itself is enabled only on the free-form
// flavors (R run output, O script output); the workspace-status panel stays
// non-searchable.

// searchableRunOutputApp builds the attachable live-run viewer flavor the
// routing tests need — no fetch plumbing, just the dialog + slot flags.
func searchableRunOutputApp(t *testing.T) *App {
	t.Helper()
	a := &App{
		toast:  common.NewToastModel(),
		width:  120,
		height: 40,
	}
	a.overlays.runOutput = common.NewOutputDialog("Run output — ws", "needle line\nfiller\n")
	a.overlays.runOutput.SetSearchable(true)
	a.overlays.runOutput.SetAttachHint(true)
	a.overlays.runOutput.SetSize(120, 40)
	a.overlays.runOutput.Show()
	a.overlays.runOutputAttachable = true
	a.overlays.runOutputWorkspace = &data.Workspace{Name: "ws"}
	a.overlays.runOutputSession = "amux-ws-x-run"
	return a
}

func keyA() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'a', Text: "a"} }

func TestRunOutputSearch_AttachBlockedWhileEditing(t *testing.T) {
	a := searchableRunOutputApp(t)
	var cmds []tea.Cmd

	// `/` enters edit mode through the normal routing path.
	if !a.handleRunOutputInput(tea.KeyPressMsg{Code: '/', Text: "/"}, &cmds) {
		t.Fatal("/ was not consumed")
	}
	if !a.overlays.runOutput.Editing() {
		t.Fatal("/ did not enter query-edit mode")
	}

	// `a` while editing is literal query text: consumed as a key, but it
	// must NOT enqueue the attach command.
	before := len(cmds)
	a.handleRunOutputInput(keyA(), &cmds)
	if len(cmds) != before {
		t.Fatal("`a` while editing enqueued the attach cmd — it is query text")
	}
	if !a.overlays.runOutput.Editing() {
		t.Fatal("`a` dropped edit mode")
	}
	if !strings.Contains(a.overlays.runOutput.View(), "/a") {
		t.Fatalf("query echo missing the literal 'a':\n%s", a.overlays.runOutput.View())
	}
}

func TestRunOutputSearch_AttachWorksWhileBrowsing(t *testing.T) {
	a := searchableRunOutputApp(t)
	var cmds []tea.Cmd
	if !a.handleRunOutputInput(keyA(), &cmds) {
		t.Fatal("browse-mode `a` not consumed")
	}
	if len(cmds) != 1 {
		t.Fatalf("browse-mode `a` enqueued %d cmds, want the attach cmd", len(cmds))
	}
}

func TestRunOutputSearch_PasteConsumedInEditFallsThroughInBrowse(t *testing.T) {
	a := searchableRunOutputApp(t)
	var cmds []tea.Cmd
	paste := tea.PasteMsg{Content: "needle"}

	// Browse mode: paste is NOT consumed — the focused terminal gets it.
	if a.handleRunOutputInput(paste, &cmds) {
		t.Fatal("browse-mode paste was consumed — it must reach the terminal")
	}

	// Edit mode: the dialog owns paste — it lands in the query and the
	// terminal never sees those bytes.
	a.handleRunOutputInput(tea.KeyPressMsg{Code: '/', Text: "/"}, &cmds)
	if !a.handleRunOutputInput(paste, &cmds) {
		t.Fatal("edit-mode paste was not consumed — it would reach the terminal")
	}
	if !strings.Contains(a.overlays.runOutput.View(), "/needle") {
		t.Fatalf("pasted text did not land in the query:\n%s", a.overlays.runOutput.View())
	}
}

// TestRunOutputSearch_EnabledOnRunViewer drives the real R-open path and
// asserts the constructed viewer is searchable + attachable.
func TestRunOutputSearch_EnabledOnRunViewer(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	runner := process.NewScriptRunner(6200, 10)
	name := "amux-ws-" + string(ws.ID()) + "-run"
	runner.SetRunHost(&stubRunSessionHost{
		tails: map[string]string{name: "server ready on :6200"},
		alive: map[string]bool{name: false},
	})
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")

	openRunOutput(t, h, ws)
	d := h.app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("run-output dialog did not open")
	}
	if !strings.Contains(d.View(), "/ search") {
		t.Fatalf("run-output viewer not searchable:\n%s", d.View())
	}
	// And the dialog accepts the search key end to end.
	var cmds []tea.Cmd
	h.app.handleRunOutputInput(tea.KeyPressMsg{Code: '/', Text: "/"}, &cmds)
	if !d.Editing() {
		t.Fatal("/ did not enter edit mode on the real run-output dialog")
	}
}

// TestRunOutputSearch_EnabledOnScriptViewer covers the O flavor: recorded
// lifecycle transcripts open in a searchable viewer.
func TestRunOutputSearch_EnabledOnScriptViewer(t *testing.T) {
	app, scripts := newScriptOutputApp(t)
	ws := &data.Workspace{
		Name: "ws", Repo: t.TempDir(), Root: t.TempDir(),
		Scripts: data.ScriptsConfig{Archive: "echo archive-marker"},
	}
	if err := scripts.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive() error = %v", err)
	}
	app.handleShowScriptOutput(messages.ShowScriptOutput{Workspace: ws})
	d := app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("script-output dialog did not open")
	}
	if !strings.Contains(d.View(), "/ search") {
		t.Fatalf("script-output viewer not searchable:\n%s", d.View())
	}
}

// TestWorkspaceStatus_NotSearchable pins the deliberately-different flavor:
// the `i` status panel is a fixed-field dashboard — `/` must not open a
// query field there.
func TestWorkspaceStatus_NotSearchable(t *testing.T) {
	app, _ := newScriptOutputApp(t)
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	app.handleWorkspaceStatusReady(workspaceStatusReadyMsg{
		token: app.overlays.runOutputToken,
		ws:    ws,
	})
	d := app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("status dialog did not open")
	}
	if strings.Contains(d.View(), "search") {
		t.Fatalf("status viewer advertises search — it must not:\n%s", d.View())
	}
	var cmds []tea.Cmd
	app.handleRunOutputInput(tea.KeyPressMsg{Code: '/', Text: "/"}, &cmds)
	if d.Editing() {
		t.Fatal("/ entered edit mode on the status viewer")
	}
}
