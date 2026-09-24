package process

import (
	"errors"
	"sort"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// fakeRunSessionHost is an in-memory RunSessionHost: sessions carry an
// alive/exitCode state so tests can simulate live, finished, and crashed runs
// without tmux.
type fakeRunSessionHost struct {
	sessions  map[string]*fakeRunSession
	ensured   []string // Ensure call order, for name assertions
	killed    []string
	findCalls []string // workspaceID per Find call
	findErr   error
	ensureErr error
}

type fakeRunSession struct {
	alive    bool
	exitCode int
	meta     RunSessionMeta
	cmd      string
	workDir  string
	env      []string
	tail     string
}

func newFakeRunSessionHost() *fakeRunSessionHost {
	return &fakeRunSessionHost{sessions: map[string]*fakeRunSession{}}
}

func (f *fakeRunSessionHost) Ensure(name, workDir, cmd string, env []string, meta RunSessionMeta) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	if _, ok := f.sessions[name]; !ok {
		f.sessions[name] = &fakeRunSession{alive: true, exitCode: -1}
		f.ensured = append(f.ensured, name)
	}
	s := f.sessions[name]
	s.meta = meta
	s.cmd = cmd
	s.workDir = workDir
	s.env = env
	return nil
}

func (f *fakeRunSessionHost) Status(name string) (exists, alive bool, exitCode int, err error) {
	s, ok := f.sessions[name]
	if !ok {
		return false, false, -1, nil
	}
	return true, s.alive, s.exitCode, nil
}

func (f *fakeRunSessionHost) Kill(name string) error {
	delete(f.sessions, name)
	f.killed = append(f.killed, name)
	return nil
}

func (f *fakeRunSessionHost) Tail(name string, _ int) string {
	if s, ok := f.sessions[name]; ok {
		return s.tail
	}
	return ""
}

func (f *fakeRunSessionHost) Find(workspaceID string) ([]string, error) {
	f.findCalls = append(f.findCalls, workspaceID)
	if f.findErr != nil {
		return nil, f.findErr
	}
	var names []string
	for name, s := range f.sessions {
		if s.meta.WorkspaceID == workspaceID {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// die simulates the session's command finishing with the given exit code:
// the session stays (remain-on-exit) but the pane is dead.
func (f *fakeRunSessionHost) die(name string, exitCode int) {
	if s, ok := f.sessions[name]; ok {
		s.alive = false
		s.exitCode = exitCode
	}
}

func newHostedWorkspace(t *testing.T, mode string) *data.Workspace {
	t.Helper()
	ws := &data.Workspace{
		Name:       "ws",
		Root:       t.TempDir(),
		Repo:       t.TempDir(),
		ScriptMode: mode,
		Scripts: data.ScriptsConfig{
			Run: "make dev",
		},
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	return ws
}

func TestRunScriptHostedEnsuresSession(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if len(host.ensured) != 1 {
		t.Fatalf("Ensure calls = %d, want 1", len(host.ensured))
	}
	name := host.ensured[0]
	want := "amux-ws-" + string(ws.ID()) + "-run"
	if name != want {
		t.Fatalf("session name = %q, want %q", name, want)
	}
	s := host.sessions[name]
	if s.cmd != "make dev" {
		t.Fatalf("session cmd = %q, want %q", s.cmd, "make dev")
	}
	if s.workDir != ws.Root {
		t.Fatalf("session workDir = %q, want %q", s.workDir, ws.Root)
	}
	if s.meta.WorkspaceID != string(ws.ID()) {
		t.Fatalf("session workspace tag = %q, want %q", s.meta.WorkspaceID, ws.ID())
	}
	if s.meta.CreatedAt == 0 {
		t.Fatal("session CreatedAt tag not stamped")
	}
	if len(s.env) == 0 {
		t.Fatal("session env empty — BuildEnv was not applied")
	}
	if !runner.IsRunning(ws) {
		t.Fatal("IsRunning() = false with a live session")
	}
}

func TestRunScriptHostedConcurrentNamesDistinct(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")

	for i := 0; i < 2; i++ {
		if _, err := runner.RunScript(ws, ScriptRun); err != nil {
			t.Fatalf("RunScript() #%d error = %v", i, err)
		}
	}
	base := "amux-ws-" + string(ws.ID()) + "-run"
	if len(host.ensured) != 2 || host.ensured[0] != base || host.ensured[1] != base+"-2" {
		t.Fatalf("ensured = %v, want [%q %q-2]", host.ensured, base, base)
	}
}

func TestRunScriptHostedNonconcurrentStopsFirst(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() second error = %v", err)
	}
	base := "amux-ws-" + string(ws.ID()) + "-run"
	if len(host.killed) != 1 || host.killed[0] != base {
		t.Fatalf("killed = %v, want [%q]", host.killed, base)
	}
	// The second run re-uses the base name (nothing was found after the kill).
	if len(host.ensured) != 2 || host.ensured[1] != base {
		t.Fatalf("ensured = %v, want second Ensure %q", host.ensured, base)
	}
}

func TestHostedStopKillsAllSessions(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")

	for i := 0; i < 2; i++ {
		if _, err := runner.RunScript(ws, ScriptRun); err != nil {
			t.Fatalf("RunScript() #%d error = %v", i, err)
		}
	}
	if err := runner.Stop(ws); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if len(host.killed) != 2 {
		t.Fatalf("killed = %v, want both run sessions", host.killed)
	}
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true after Stop killed all sessions")
	}
}

func TestHostedStopSurvivesFindError(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")
	host.findErr = errors.New("tmux down")
	if err := runner.Stop(ws); err == nil {
		t.Fatal("Stop() error = nil, want find failure propagated")
	}
}

func TestHostedRunScriptStatusReportsExit(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	name := host.ensured[0]
	host.die(name, 7)

	alive, lastExit := runner.RunScriptStatus(ws)
	if alive {
		t.Fatal("RunScriptStatus alive = true after session died")
	}
	if lastExit != 7 {
		t.Fatalf("RunScriptStatus lastExit = %d, want 7", lastExit)
	}
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true after session died")
	}
}

func TestHostedRunScriptOutputTailsLatestSession(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "concurrent")

	for i := 0; i < 2; i++ {
		if _, err := runner.RunScript(ws, ScriptRun); err != nil {
			t.Fatalf("RunScript() #%d error = %v", i, err)
		}
	}
	base := "amux-ws-" + string(ws.ID()) + "-run"
	host.sessions[base].tail = "first"
	host.sessions[base+"-2"].tail = "second"
	if got := runner.RunScriptOutput(ws, 50); got != "second" {
		t.Fatalf("RunScriptOutput() = %q, want latest session's tail %q", got, "second")
	}
}

// TestHostedRunScriptOutputAndStatusSingleSweep pins the plan-131 dedupe:
// the combined fetch must spend ONE hosted sweep (one Find), not the two
// the RunScriptOutput + RunScriptStatus pair costs.
func TestHostedRunScriptOutputAndStatusSingleSweep(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	name := host.ensured[0]
	host.sessions[name].tail = "tail-content"
	host.findCalls = nil

	output, alive, lastExit := runner.RunScriptOutputAndStatus(ws, 50)
	if got := len(host.findCalls); got != 1 {
		t.Fatalf("RunScriptOutputAndStatus ran %d sweeps, want 1", got)
	}
	if output != "tail-content" {
		t.Fatalf("output = %q, want %q", output, "tail-content")
	}
	if !alive {
		t.Fatal("alive = false for a live session")
	}
	if lastExit != -1 {
		t.Fatalf("lastExit = %d, want -1 with no dead session", lastExit)
	}

	// Dead-session half: exit code surfaces, tail still comes through.
	host.die(name, 9)
	host.findCalls = nil
	output, alive, lastExit = runner.RunScriptOutputAndStatus(ws, 50)
	if got := len(host.findCalls); got != 1 {
		t.Fatalf("dead-session fetch ran %d sweeps, want 1", got)
	}
	if alive || lastExit != 9 {
		t.Fatalf("(alive, lastExit) = (%v, %d), want (false, 9)", alive, lastExit)
	}
	if output != "tail-content" {
		t.Fatalf("remain-on-exit tail = %q, want %q", output, "tail-content")
	}
}

// TestHostedRunScriptOutputAndStatusNoSession covers the empty-find half:
// one sweep, empty output, not alive.
func TestHostedRunScriptOutputAndStatusNoSession(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")
	// Force the sweep path past the seen-set gate: no configured script AND
	// never-seen short-circuits without a Find; give it a dead record by
	// ensuring then deleting.
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	delete(host.sessions, host.ensured[0])
	host.findCalls = nil

	output, alive, lastExit := runner.RunScriptOutputAndStatus(ws, 50)
	if got := len(host.findCalls); got != 1 {
		t.Fatalf("empty fetch ran %d sweeps, want 1", got)
	}
	if output != "" || alive || lastExit != -1 {
		t.Fatalf("(output, alive, lastExit) = (%q, %v, %d), want (\"\", false, -1)", output, alive, lastExit)
	}
}

func TestHostedReleaseWorkspaceDefersWhileAlive(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if _, held := runner.PortAllocated(ws); !held {
		t.Fatal("port not allocated after RunScript")
	}
	runner.ReleaseWorkspace(ws)
	if _, held := runner.PortAllocated(ws); !held {
		t.Fatal("port released while hosted session still alive")
	}
	// Session dies on its own; the IsRunning poll (the app's indicator sync)
	// is the sweep point that drains the parked release.
	host.die(host.ensured[0], 0)
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true after session died")
	}
	if _, held := runner.PortAllocated(ws); held {
		t.Fatal("port still held after last session died and release swept")
	}
}

func TestUnhostedStatusAndOutputFallback(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")

	alive, lastExit := runner.RunScriptStatus(ws)
	if alive || lastExit != -1 {
		t.Fatalf("RunScriptStatus() = (%v, %d), want (false, -1) with no host", alive, lastExit)
	}
	if got := runner.RunScriptOutput(ws, 10); got != "" {
		t.Fatalf("RunScriptOutput() = %q, want empty with no host", got)
	}
}

// TestFindRunSessions_DedupesIdentityForms pins plan 086: the per-poll Find
// sweep must issue one host call per *distinct* workspace identity form, not
// per form in the list — a workspace whose MetadataID() == ID() used to pay
// two identical tmux list-sessions subprocesses every 3s.
func TestFindRunSessions_DedupesIdentityForms(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)

	// Unsaved workspace: MetadataID == ID == ComputedID → exactly one Find.
	ws := newHostedWorkspace(t, "nonconcurrent")
	if ws.MetadataID() != ws.ID() || ws.ID() != ws.ComputedID() {
		t.Fatal("fixture invalid: unsaved workspace should have a single identity form")
	}
	if _, err := runner.findRunSessions(ws); err != nil {
		t.Fatalf("findRunSessions() error = %v", err)
	}
	if got := len(host.findCalls); got != 1 {
		t.Fatalf("Find calls = %d, want 1 for a single identity form", got)
	}

	// Persisted workspace with a drifted root: MetadataID == ID (the minted
	// store key) but ComputedID differs → two Finds, one per distinct form.
	store := data.NewWorkspaceStore(t.TempDir())
	saved := newHostedWorkspace(t, "nonconcurrent")
	if err := store.Save(saved); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	saved.Root = t.TempDir() // drift the computed identity away from the store key
	if saved.MetadataID() != saved.ID() || saved.ID() == saved.ComputedID() {
		t.Fatal("fixture invalid: expected persisted ID distinct from ComputedID")
	}
	host.findCalls = nil
	if _, err := runner.findRunSessions(saved); err != nil {
		t.Fatalf("findRunSessions() error = %v", err)
	}
	if got := len(host.findCalls); got != 2 {
		t.Fatalf("Find calls = %d, want 2 (store ID + drifted computed ID)", got)
	}
}

// TestRunScriptStatus_SkipsSweepWhenNothingCanRun pins the plan-086 early-out:
// a workspace with no configured run script and no previously observed session
// must not pay the tmux Find sweep on every poll — while a workspace whose
// script was removed mid-session keeps sweeping until the session is gone.
func TestRunScriptStatus_SkipsSweepWhenNothingCanRun(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)

	// No repo config and no ws.Scripts.Run → nothing can be running → no Find.
	bare := &data.Workspace{Name: "bare", Root: t.TempDir(), Repo: t.TempDir()}
	runner.RunScriptStatus(bare)
	if got := len(host.findCalls); got != 0 {
		t.Fatalf("Find calls = %d, want 0 for a workspace that cannot have a session", got)
	}

	// Configured workspace runs a session; the sweep observes it.
	ws := newHostedWorkspace(t, "nonconcurrent")
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	host.findCalls = nil
	runner.RunScriptStatus(ws)
	if got := len(host.findCalls); got == 0 {
		t.Fatal("expected the sweep to run for a workspace with a live session")
	}

	// Config removed while the session still lives → the sweep must continue
	// so the marker can clear on exit.
	ws.Scripts.Run = ""
	host.findCalls = nil
	runner.RunScriptStatus(ws)
	if got := len(host.findCalls); got == 0 {
		t.Fatal("config removed mid-session: sweep skipped, marker would stick")
	}
}
