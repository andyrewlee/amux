package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// gatedWorkspaceStore wraps a real WorkspaceStore, parking each mutating
// call on a release gate. A test asserts the handler returns before the
// transaction runs — the flock never touches the Update goroutine — then
// releases the gate and delivers the result msg.
type gatedWorkspaceStore struct {
	workspacesvc.WorkspaceStore
	release chan struct{}
	calls   atomic.Int32
}

func newGatedWorkspaceStore(inner workspacesvc.WorkspaceStore) *gatedWorkspaceStore {
	return &gatedWorkspaceStore{WorkspaceStore: inner, release: make(chan struct{})}
}

func (s *gatedWorkspaceStore) gate() {
	s.calls.Add(1)
	<-s.release
}

func (s *gatedWorkspaceStore) Rename(id data.WorkspaceID, newName string) error {
	s.gate()
	return s.WorkspaceStore.Rename(id, newName)
}

func (s *gatedWorkspaceStore) SetEnv(id data.WorkspaceID, env map[string]string) error {
	s.gate()
	return s.WorkspaceStore.SetEnv(id, env)
}

func (s *gatedWorkspaceStore) SetScripts(id data.WorkspaceID, scripts data.ScriptsConfig, mode string) error {
	s.gate()
	return s.WorkspaceStore.SetScripts(id, scripts, mode)
}

// failingWorkspaceStore returns a fixed error from every mutation.
type failingWorkspaceStore struct {
	workspacesvc.WorkspaceStore
	err error
}

func (s *failingWorkspaceStore) Rename(data.WorkspaceID, string) error {
	return s.err
}

func (s *failingWorkspaceStore) SetEnv(data.WorkspaceID, map[string]string) error {
	return s.err
}

// gatedProjectEnv fakes projectEnvIO with the same gate.
type gatedProjectEnv struct {
	mu      sync.Mutex
	env     map[string]map[string]string
	release chan struct{}
	calls   atomic.Int32
}

func (s *gatedProjectEnv) ForRepo(repo string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.env[repo]
}

func (s *gatedProjectEnv) Set(repo string, env map[string]string) error {
	s.calls.Add(1)
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.env == nil {
		s.env = map[string]map[string]string{}
	}
	s.env[repo] = env
	return nil
}

func waitForStoreCalls(t *testing.T, calls *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < want {
		t.Fatalf("store not reached within 2s: calls=%d, want %d", calls.Load(), want)
	}
}

// runAsyncWrite runs cmd off the test goroutine, proves the store parked
// inside its gate, then releases and returns the emitted result msg.
func runAsyncWrite(t *testing.T, cmd tea.Cmd, calls *atomic.Int32, release chan struct{}) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	waitForStoreCalls(t, calls, 1)
	close(release)
	select {
	case msg := <-done:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("write cmd did not finish after gate release")
		return nil
	}
}

// TestRenameWorkspaceRunsOffUpdateLoop proves handleRenameWorkspace dispatches
// the flock-backed store transaction as a Cmd instead of running it inline:
// the handler returns with zero store calls, the transaction parks on the
// gate, and only the delivered result applies the reflect + reload.
func TestRenameWorkspaceRunsOffUpdateLoop(t *testing.T) {
	ws := &data.Workspace{Name: "old", Repo: "/repo", Root: "/repo/ws"}
	h, store, id := newEnvTestHarness(t, ws)
	gate := newGatedWorkspaceStore(store)
	h.app.workspaceService = workspacesvc.New(nil, gate, nil, "")
	h.app.activeWorkspace = ws

	cmds := h.app.handleRenameWorkspace(messages.RenameWorkspace{Workspace: ws, NewName: "new"})
	if len(cmds) != 1 || cmds[0] == nil {
		t.Fatalf("expected the async rename cmd, got %v", cmds)
	}
	if got := gate.calls.Load(); got != 0 {
		t.Fatalf("store called inline on Update: %d calls", got)
	}

	msg := runAsyncWrite(t, cmds[0], &gate.calls, gate.release)
	res, ok := msg.(renameWorkspaceResultMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want renameWorkspaceResultMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("rename err = %v", res.err)
	}
	if h.app.activeWorkspace.Name != "old" {
		t.Fatal("reflect applied before the confirmed result")
	}

	h.app.Update(res)

	if h.app.activeWorkspace.Name != "new" {
		t.Fatalf("active workspace Name = %q, want %q", h.app.activeWorkspace.Name, "new")
	}
	stored, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if stored.Name != "new" {
		t.Fatalf("persisted Name = %q, want %q", stored.Name, "new")
	}
}

// TestWorkspaceEnvWriteRunsOffUpdateLoop proves the env dialog's persist path
// moves through the async cmd: the handler parks on the gate, and the
// confirmed result applies the in-memory reflect.
func TestWorkspaceEnvWriteRunsOffUpdateLoop(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws", Env: map[string]string{"K": "v"}}
	h, store, id := newEnvTestHarness(t, ws)
	gate := newGatedWorkspaceStore(store)
	h.app.workspaceService = workspacesvc.New(nil, gate, nil, "")
	h.app.activeWorkspace = ws

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected the async save cmd")
	}
	if got := gate.calls.Load(); got != 0 {
		t.Fatalf("store called inline on Update: %d calls", got)
	}

	msg := runAsyncWrite(t, cmd, &gate.calls, gate.release)
	res, ok := msg.(workspaceEnvSavedMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want workspaceEnvSavedMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("SetEnv err = %v", res.err)
	}

	h.app.Update(res)

	if h.app.activeWorkspace.Env["K"] != "vX" {
		t.Fatalf("active workspace Env not reflected: %#v", h.app.activeWorkspace.Env)
	}
	stored, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if stored.Env["K"] != "vX" {
		t.Fatalf("persisted Env K = %q, want %q", stored.Env["K"], "vX")
	}
}

// TestWorkspaceScriptsWriteRunsOffUpdateLoop proves the scripts dialog's
// persist path is async, and the result reflects Scripts + ScriptMode.
func TestWorkspaceScriptsWriteRunsOffUpdateLoop(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	h, store, id := newScriptsTestHarness(t, ws)
	gate := newGatedWorkspaceStore(store)
	h.app.workspaceService = workspacesvc.New(nil, gate, nil, "")
	h.app.activeWorkspace = ws

	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})
	h.app.overlays.scripts.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})

	cmd := h.app.handleScriptsDialogResult(common.ScriptsDialogResult{})
	if cmd == nil {
		t.Fatal("expected the async save cmd")
	}
	if got := gate.calls.Load(); got != 0 {
		t.Fatalf("store called inline on Update: %d calls", got)
	}

	msg := runAsyncWrite(t, cmd, &gate.calls, gate.release)
	res, ok := msg.(workspaceScriptsSavedMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want workspaceScriptsSavedMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("SetScripts err = %v", res.err)
	}

	h.app.Update(res)

	if h.app.activeWorkspace.Scripts.Setup != "m" {
		t.Fatalf("active workspace Scripts not reflected: %#v", h.app.activeWorkspace.Scripts)
	}
	stored, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if stored.Scripts.Setup != "m" {
		t.Fatalf("persisted Scripts.Setup = %q, want %q", stored.Scripts.Setup, "m")
	}
}

// TestProjectEnvWriteRunsOffUpdateLoop proves ProjectEnvStore.Set parks
// behind the async cmd rather than the Update goroutine.
func TestProjectEnvWriteRunsOffUpdateLoop(t *testing.T) {
	repo := t.TempDir()
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: repo + "/ws"}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	gate := &gatedProjectEnv{
		env:     map[string]map[string]string{repo: {"K": "v"}},
		release: make(chan struct{}),
	}
	h.app.projectEnvStore = gate

	deliverCmdMsgs(t, h.app, h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: ws}))
	h.app.overlays.projectEnv.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleProjectEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected the async save cmd")
	}
	if got := gate.calls.Load(); got != 0 {
		t.Fatalf("store called inline on Update: %d calls", got)
	}

	msg := runAsyncWrite(t, cmd, &gate.calls, gate.release)
	res, ok := msg.(projectEnvSavedMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want projectEnvSavedMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("Set err = %v", res.err)
	}
	if got := gate.ForRepo(repo)["K"]; got != "vX" {
		t.Fatalf("persisted K = %q, want %q", got, "vX")
	}
}

// TestWorkspaceEnvSavedErrorSurfacesReportError proves a failed transaction
// produces the same ReportError surface the inline version did — one Update
// tick later — and applies no reflect.
func TestWorkspaceEnvSavedErrorSurfacesReportError(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws", Env: map[string]string{"K": "v"}}
	h, store, _ := newEnvTestHarness(t, ws)
	fail := &failingWorkspaceStore{WorkspaceStore: store, err: errors.New("disk full")}
	h.app.workspaceService = workspacesvc.New(nil, fail, nil, "")
	h.app.activeWorkspace = ws

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected the async save cmd")
	}
	msg, ok := cmd().(workspaceEnvSavedMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want workspaceEnvSavedMsg", msg)
	}
	if msg.err == nil {
		t.Fatal("expected the store error on the result")
	}

	msgs := runCommandMessages(mustResultCmd(t, h.app, msg))
	var sawError bool
	for _, m := range msgs {
		if _, ok := m.(messages.Error); ok {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected messages.Error from ReportError, got %v", msgs)
	}
	if h.app.activeWorkspace.Env["K"] != "v" {
		t.Fatalf("failed write must not reflect: %#v", h.app.activeWorkspace.Env)
	}
}

// mustResultCmd delivers a result msg through Update and returns the cmd the
// result handler produced.
func mustResultCmd(t *testing.T, app *App, msg tea.Msg) tea.Cmd {
	t.Helper()
	_, cmd := app.Update(msg)
	return cmd
}

// deliverCmdMsgs runs cmd once and feeds every emitted message back through
// app.Update — the runtime's cmd→msg→Update pump for async writes. Follow-up
// cmds the delivery returns (toast ticks, reloads) are left for the runtime:
// executing them would sleep out timers and fire loads tests don't await.
func deliverCmdMsgs(t *testing.T, app *App, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	msgs := runCommandMessages(cmd)
	for _, msg := range msgs {
		app.Update(msg)
	}
	return msgs
}
