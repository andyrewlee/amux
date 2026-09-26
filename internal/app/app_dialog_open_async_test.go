package app

import (
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// gatedRunHost wraps stubRunSessionHost, recording every host call and
// blocking Tail/Status until gate is closed — proof that a Show* handler
// never touches the host synchronously on the Update loop.
type gatedRunHost struct {
	stub  stubRunSessionHost
	gate  chan struct{}
	calls atomic.Int32
}

func (g *gatedRunHost) Ensure(string, string, string, []string, process.RunSessionMeta) error {
	return nil
}

func (g *gatedRunHost) Status(name string) (bool, bool, int, error) {
	g.calls.Add(1)
	<-g.gate
	return g.stub.Status(name)
}

func (g *gatedRunHost) Kill(string) error { return nil }

func (g *gatedRunHost) Tail(name string, n int) string {
	g.calls.Add(1)
	<-g.gate
	return g.stub.Tail(name, n)
}

func (g *gatedRunHost) Find(name string) ([]string, error) {
	g.calls.Add(1)
	<-g.gate
	return g.stub.Find(name)
}

// TestShowRunScriptOutput_DoesNotBlockOnHost pins the defect class
// for the R-open path: no RunSessionHost method runs inside the handler —
// the returned cmd does the reads off-loop.
func TestShowRunScriptOutput_DoesNotBlockOnHost(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	runner := process.NewScriptRunner(6200, 10)
	name := "amux-ws-" + string(ws.ID()) + "-run"
	host := &gatedRunHost{
		stub: stubRunSessionHost{
			tails: map[string]string{name: "server ready"},
			alive: map[string]bool{name: true},
		},
		gate: make(chan struct{}),
	}
	runner.SetRunHost(host)
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, runner, "")

	cmd := h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the open handler to return a fetch cmd")
	}
	if host.calls.Load() != 0 {
		t.Fatalf("handler invoked the host synchronously (%d calls)", host.calls.Load())
	}
	if h.app.overlays.runOutput != nil {
		t.Fatal("dialog must not open before the fetch resolves")
	}

	// The enumerate cmd does the host reads off-loop, then the tail fetch —
	// both stages stay off the Update goroutine.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	close(host.gate)
	enum, ok := (<-done).(runSessionsEnumeratedMsg)
	if !ok {
		t.Fatalf("open cmd emitted %T, want runSessionsEnumeratedMsg", <-done)
	}
	fetch := h.app.handleRunSessionsEnumerated(enum)
	if fetch == nil {
		t.Fatal("single-session enumeration should yield a tail fetch")
	}
	opened, ok := fetch().(runOutputOpenedMsg)
	if !ok {
		t.Fatalf("fetch emitted %T, want runOutputOpenedMsg", fetch())
	}
	if host.calls.Load() == 0 {
		t.Fatal("fetch cmd never invoked the host")
	}
	apply := h.app.handleRunOutputOpened(opened)
	if h.app.overlays.runOutput == nil || !h.app.overlays.runOutput.Visible() {
		t.Fatal("expected runOutputDialog after the opened msg applied")
	}
	if apply == nil {
		t.Fatal("expected the alive session to arm a refresh tick")
	}
}

// TestShowWorkspaceStatus_StatusReadOffLoop pins the same guarantee for the
// i-open path: the RunScriptStatus subprocess read happens in the cmd.
func TestShowWorkspaceStatus_StatusReadOffLoop(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	runner := process.NewScriptRunner(6200, 10)
	host := &gatedRunHost{stub: stubRunSessionHost{}, gate: make(chan struct{})}
	runner.SetRunHost(host)
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, runner, ""),
		width:            120,
		height:           40,
	}

	cmd := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the status open to return a fetch cmd")
	}
	if host.calls.Load() != 0 {
		t.Fatalf("handler invoked the host synchronously (%d calls)", host.calls.Load())
	}
	if app.overlays.runOutput != nil {
		t.Fatal("dialog must not open before the fetch resolves")
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	close(host.gate)
	ready, ok := (<-done).(workspaceStatusReadyMsg)
	if !ok {
		t.Fatal("fetch cmd did not emit workspaceStatusReadyMsg")
	}
	app.handleWorkspaceStatusReady(ready)
	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("status dialog did not open after the ready msg")
	}
	if !strings.Contains(app.overlays.runOutput.View(), "identity") {
		t.Fatal("status dialog missing composed content")
	}
}

// TestRunOutputOpened_StaleTokenDropped pins the open-time token guard: a
// second R press bumps runOutputToken, and the superseded fetch applies to
// nothing.
func TestRunOutputOpened_StaleTokenDropped(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws", Scripts: data.ScriptsConfig{Run: "make dev"}}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	runner := process.NewScriptRunner(6200, 10)
	name := "amux-ws-" + string(ws.ID()) + "-run"
	runner.SetRunHost(&stubRunSessionHost{
		tails: map[string]string{name: "tail"},
		alive: map[string]bool{name: false},
	})
	h.app.workspaceService = workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), runner, "")

	first := h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws})
	_ = h.app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws}) // bumps token

	enum, ok := first().(runSessionsEnumeratedMsg)
	if !ok {
		t.Fatalf("open cmd emitted %T, want runSessionsEnumeratedMsg", first())
	}
	if cmd := h.app.handleRunSessionsEnumerated(enum); cmd != nil {
		t.Fatal("stale enumeration must be dropped before the tail fetch")
	}
	if h.app.overlays.runOutput != nil {
		t.Fatal("stale open opened a dialog")
	}
}
