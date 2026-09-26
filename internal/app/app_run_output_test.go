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

// stubRunSessionHost answers hosted-run queries from a fixed map so app tests
// can exercise the output-viewer path without tmux.
type stubRunSessionHost struct {
	tails map[string]string
	alive map[string]bool
	exits map[string]int
}

func (s *stubRunSessionHost) Ensure(string, string, string, []string, process.RunSessionMeta) error {
	return nil
}

func (s *stubRunSessionHost) Status(name string) (bool, bool, int, error) {
	alive, ok := s.alive[name]
	if !ok {
		return false, false, -1, nil
	}
	code := -1
	if !alive {
		code = s.exits[name]
	}
	return true, alive, code, nil
}
func (s *stubRunSessionHost) Kill(string) error              { return nil }
func (s *stubRunSessionHost) Tail(name string, _ int) string { return s.tails[name] }
func (s *stubRunSessionHost) Find(string) ([]string, error) {
	names := make([]string, 0, len(s.alive))
	for n := range s.alive {
		names = append(names, n)
	}
	return names, nil
}

// openRunOutput drives the two-stage open path end to end: the Show handler
// returns an enumerate cmd; the enumerated result then either toasts/opens a
// picker (nil return) or yields the tail fetch whose message the open handler
// applies.
func openRunOutput(t *testing.T, h *Harness, ws *data.Workspace) tea.Cmd {
	t.Helper()
	cmd := h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the open handler to return an enumerate cmd")
	}
	enum, ok := cmd().(runSessionsEnumeratedMsg)
	if !ok {
		t.Fatalf("open cmd emitted %T, want runSessionsEnumeratedMsg", cmd())
	}
	fetch := h.app.handleRunSessionsEnumerated(enum)
	if len(enum.entries) != 1 {
		// Empty enumeration returns a toast timer cmd (which blocks on its
		// dismissal tick if invoked); the picker path returns nil. Only the
		// single-session path yields a tail fetch safe to call inline.
		return nil
	}
	if fetch == nil {
		t.Fatal("single-session enumeration should yield a tail fetch")
	}
	msg, ok := fetch().(runOutputOpenedMsg)
	if !ok {
		t.Fatalf("fetch cmd emitted %T, want runOutputOpenedMsg", fetch())
	}
	return h.app.handleRunOutputOpened(msg)
}

func TestHandleShowRunScriptOutput_OpensViewerWithTail(t *testing.T) {
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
	if h.app.overlays.runOutput == nil || !h.app.overlays.runOutput.Visible() {
		t.Fatal("expected runOutputDialog to be shown")
	}
	if view := h.app.overlays.runOutput.View(); !strings.Contains(view, "server ready on :6200") {
		t.Fatalf("viewer missing captured output, got %q", view)
	}
}

func TestHandleShowRunScriptOutput_EmptyToastsInsteadOfDialog(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), process.NewScriptRunner(6200, 10), "")

	openRunOutput(t, h, ws)
	if h.app.overlays.runOutput != nil {
		t.Fatal("expected no dialog when there is no captured output")
	}
	if !strings.Contains(h.app.toast.View(), "No run output") {
		t.Fatalf("expected a 'No run output' toast, got %q", h.app.toast.View())
	}
}

func TestHandleShowRunScriptOutput_NilWorkspaceIsNoop(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: nil})
	if h.app.overlays.runOutput != nil {
		t.Fatal("expected no dialog for a nil workspace")
	}
}

// newRunOutputHarness builds the stubbed-host harness the refresh tests
// share: a workspace whose run session reports alive=alive and whose tail
// comes from tails[name].
func newRunOutputHarness(t *testing.T, ws *data.Workspace, tail string, alive bool) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	runner := process.NewScriptRunner(6200, 10)
	name := "amux-ws-" + string(ws.ID()) + "-run"
	runner.SetRunHost(&stubRunSessionHost{
		tails: map[string]string{name: tail},
		alive: map[string]bool{name: alive},
	})
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")
	return h
}

// TestHandleShowRunScriptOutput_ArmsRefreshOnlyWhileAlive pins the loop's
// entry condition: an alive session schedules the first tick; a dead one
// leaves the static remain-on-exit snapshot alone.
func TestHandleShowRunScriptOutput_ArmsRefreshOnlyWhileAlive(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}

	live := newRunOutputHarness(t, ws, "tick-1", true)
	if cmd := openRunOutput(t, live, ws); cmd == nil {
		t.Fatal("expected a scheduled refresh tick for a live session")
	}
	if live.app.overlays.runOutputWorkspace != ws {
		t.Fatal("expected runOutputWorkspace stashed for the refresh loop")
	}

	dead := newRunOutputHarness(t, ws, "final-tail", false)
	if cmd := openRunOutput(t, dead, ws); cmd != nil {
		t.Fatal("expected no refresh tick for a dead session")
	}
}

// TestHandleRunOutputTick_FetchesAndRefreshes drives one loop iteration: the
// tick becomes an off-loop fetch whose result applies content and re-arms
// while the session is alive.
func TestHandleRunOutputTick_FetchesAndRefreshes(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h := newRunOutputHarness(t, ws, "tick-1", true)
	openRunOutput(t, h, ws)

	fetch := h.app.handleRunOutputTick(runOutputTickMsg{token: h.app.overlays.runOutputToken})
	if fetch == nil {
		t.Fatal("live tick should produce a fetch cmd")
	}
	msg, ok := fetch().(runOutputRefreshedMsg)
	if !ok {
		t.Fatalf("fetch cmd emitted %T, want runOutputRefreshedMsg", fetch())
	}
	if !msg.alive || msg.content != "tick-1" {
		t.Fatalf("refreshed msg = %+v, want content=tick-1 alive=true", msg)
	}

	next := h.app.handleRunOutputRefreshed(msg)
	if next == nil {
		t.Fatal("alive refresh should re-arm the tick")
	}
}

// TestHandleRunOutputRefreshed_StopsLoopOnDeadSession covers the exit path:
// the last fetch applies the remain-on-exit tail, then no further tick is
// scheduled.
func TestHandleRunOutputRefreshed_StopsLoopOnDeadSession(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h := newRunOutputHarness(t, ws, "final tail", true)
	openRunOutput(t, h, ws)

	cmd := h.app.handleRunOutputRefreshed(runOutputRefreshedMsg{
		token:   h.app.overlays.runOutputToken,
		content: "crashed — exit 1",
		alive:   false,
	})
	if cmd != nil {
		t.Fatal("dead-session refresh must not re-arm the tick")
	}
	if view := h.app.overlays.runOutput.View(); !strings.Contains(view, "crashed — exit 1") {
		t.Fatalf("final tail not applied, got:\n%s", view)
	}
}

// TestHandleRunOutputTick_DropsStaleAndClosed pins both guards: a tick whose
// token predates the current dialog instance, and any tick after close.
func TestHandleRunOutputTick_DropsStaleAndClosed(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h := newRunOutputHarness(t, ws, "tick-1", true)
	openRunOutput(t, h, ws)

	if cmd := h.app.handleRunOutputTick(runOutputTickMsg{token: h.app.overlays.runOutputToken - 1}); cmd != nil {
		t.Fatal("stale-token tick must be dropped")
	}

	h.app.closeRunOutputDialog()
	if cmd := h.app.handleRunOutputTick(runOutputTickMsg{token: h.app.overlays.runOutputToken}); cmd != nil {
		t.Fatal("tick after close must be dropped")
	}
	if h.app.overlays.runOutput != nil || h.app.overlays.runOutputWorkspace != nil {
		t.Fatal("close did not clear the dialog/workspace")
	}

	// A refresh in flight at close also dies on the token guard.
	if cmd := h.app.handleRunOutputRefreshed(runOutputRefreshedMsg{
		token: h.app.overlays.runOutputToken - 1, content: "late", alive: true,
	}); cmd != nil {
		t.Fatal("late refresh must be dropped")
	}
}

// TestRunOutputAttachKey pins the `a` intercept in the live-run flavor of the
// shared OutputDialog: it resolves the newest alive session off-loop, then
// closes the overlay and opens a run-viewer tab in the center.
func TestRunOutputAttachKey_DispatchesAttach(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	name := "amux-ws-" + string(ws.ID()) + "-run"
	runner := process.NewScriptRunner(6200, 10)
	runner.SetRunHost(&stubRunSessionHost{
		tails: map[string]string{name: "server ready"},
		alive: map[string]bool{name: true},
	})
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")

	openRunOutput(t, h, ws)
	if !h.app.overlays.runOutputAttachable {
		t.Fatal("live-run viewer must mark itself attachable")
	}

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
	if !msg.ok || msg.name != name {
		t.Fatalf("target = %q,%v; want %q,true", msg.name, msg.ok, name)
	}

	// Applying the target closes the overlay and returns the center create cmd.
	if cmd := h.app.handleRunAttachTarget(msg); cmd == nil {
		t.Fatal("expected a center create cmd for a live target")
	}
	if h.app.overlays.runOutput != nil {
		t.Fatal("overlay should be closed once the tab takes over")
	}
}

func TestRunOutputAttachKey_NoneAliveToasts(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	name := "amux-ws-" + string(ws.ID()) + "-run"
	runner := process.NewScriptRunner(6200, 10)
	runner.SetRunHost(&stubRunSessionHost{
		tails: map[string]string{name: "finished"},
		alive: map[string]bool{name: false},
	})
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")

	if cmd := h.app.handleRunAttachTarget(runAttachTargetMsg{ws: ws, ok: false}); cmd == nil {
		t.Fatal("expected a toast cmd when nothing is alive")
	}
	if !strings.Contains(h.app.toast.View(), "No live run session") {
		t.Fatalf("expected 'No live run session' toast, got %q", h.app.toast.View())
	}
}

func TestRunOutputAttachKey_NonRunFlavorPassesThrough(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	// The workspace-status/script-output flavors share the dialog type but
	// must never attach — the flag is the discriminator.
	h.app.overlays.runOutput = common.NewOutputDialog("Workspace — ws", "status")
	h.app.overlays.runOutput.SetSize(120, 40)
	h.app.overlays.runOutput.Show()

	var cmds []tea.Cmd
	if !h.app.handleRunOutputInput(tea.KeyPressMsg{Code: 'a', Text: "a"}, &cmds) {
		t.Fatal("dialog still consumes the key")
	}
	if len(cmds) != 0 {
		t.Fatalf("non-run flavor must not emit attach cmds, got %d", len(cmds))
	}
}
