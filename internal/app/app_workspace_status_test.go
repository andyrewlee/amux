package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func writeRepoConfig(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".amux"), 0o755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

// TestBuildWorkspaceStatus_FullSnapshot proves the assembly aggregates the
// operational state: port reservation, recorded lifecycle output, script
// sources, trust verdict, merged env key names, and identity.
func TestBuildWorkspaceStatus_FullSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the trust registry (~/.amux)
	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"archive": "echo a", "env": {"REPO_KEY": "v", "SHARED": "repo"}}`)

	scripts := process.NewScriptRunner(6200, 10)
	envStore := data.NewProjectEnvStore(t.TempDir())
	if err := envStore.Set(repo, map[string]string{"PROJ_KEY": "p", "SHARED": "proj"}); err != nil {
		t.Fatalf("env store Set: %v", err)
	}
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		projectEnvStore:  envStore,
	}
	// Trust the repo so the status can report "trusted" and the archive runs.
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	ws := &data.Workspace{
		Name: "ws", Repo: repo, Root: t.TempDir(), Branch: "feat",
		Env:     map[string]string{"WS_KEY": "w", "SHARED": "ws"},
		Scripts: data.ScriptsConfig{OnDone: "echo done"},
	}
	if err := scripts.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive() error = %v", err)
	}

	st := app.buildWorkspaceStatus(ws)

	if st.name != "ws" || st.branch != "feat" || st.root != ws.Root {
		t.Fatalf("identity fields wrong: %+v", st)
	}
	// The port interval is filled by the ready message (authoritative
	// non-allocating lookup), not the synchronous build — a build-time value
	// here would mean status reads still hit the transient getter.
	if st.portAllocated {
		t.Fatalf("buildWorkspaceStatus filled port fields: %+v", st)
	}
	if st.scriptSources[process.ScriptArchive] != "repo" || st.scriptSources[process.ScriptOnDone] != "user" {
		t.Fatalf("script sources = %v, want archive=repo on-done=user", st.scriptSources)
	}
	if !st.repoConfig || !st.repoTrusted {
		t.Fatalf("repoConfig=%v repoTrusted=%v, want both true", st.repoConfig, st.repoTrusted)
	}
	// Env key names merged across all three layers, workspace winning labels.
	src := st.envKeySource
	if src["REPO_KEY"] != "repo" || src["PROJ_KEY"] != "project" || src["WS_KEY"] != "workspace" {
		t.Fatalf("env key sources = %v", src)
	}
	if src["SHARED"] != "repo" {
		t.Fatalf("SHARED source = %q, want repo (lowest layer keeps its label)", src["SHARED"])
	}
	if len(st.lifecycleOutputs) != 1 || st.lifecycleOutputs[0] != process.ScriptArchive {
		t.Fatalf("lifecycleOutputs = %v, want [archive]", st.lifecycleOutputs)
	}
}

// TestBuildWorkspaceStatus_UntrustedRepo proves the trust verdict reaches
// the surface: an untrusted repo config renders as gated, and its script
// sources still show where commands would come from.
func TestBuildWorkspaceStatus_UntrustedRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the trust registry (~/.amux)
	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"archive": "echo a"}`)
	scripts := process.NewScriptRunner(6200, 10)
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
	}
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: t.TempDir()}

	st := app.buildWorkspaceStatus(ws)
	if !st.repoConfig || st.repoTrusted {
		t.Fatalf("repoConfig=%v repoTrusted=%v, want config=true trusted=false", st.repoConfig, st.repoTrusted)
	}
	rendered := renderWorkspaceStatus(st)
	if !strings.Contains(rendered, "NOT trusted") {
		t.Fatalf("render missing trust warning:\n%s", rendered)
	}
}

// TestBuildWorkspaceStatus_ProjectScriptLayer proves the `i` dialog names the
// project layer: repo → workspace → project precedence assigns each type the
// strongest layer that defines it, and the label matches the rendered string.
func TestBuildWorkspaceStatus_ProjectScriptLayer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"archive": "echo a"}`)

	scripts := process.NewScriptRunner(6200, 10)
	scriptStore := data.NewProjectScriptStore(t.TempDir())
	if err := scriptStore.Set(repo, data.ScriptsConfig{Run: "make dev", OnDone: "make done"}); err != nil {
		t.Fatalf("script store Set: %v", err)
	}
	app := &App{
		config:             &config.Config{PortRangeSize: 10},
		toast:              common.NewToastModel(),
		workspaceService:   workspacesvc.New(nil, nil, scripts, ""),
		projectScriptStore: scriptStore,
	}
	ws := &data.Workspace{
		Name: "ws", Repo: repo, Root: t.TempDir(), Branch: "feat",
		Scripts: data.ScriptsConfig{OnDone: "echo ws-done"},
	}

	st := app.buildWorkspaceStatus(ws)

	// archive: repo claims it (only layer). on-done: ws beats project.
	// run: project claims it (repo+ws miss). setup: unclaimed everywhere.
	want := map[process.ScriptType]string{
		process.ScriptSetup:   "",
		process.ScriptRun:     "project",
		process.ScriptArchive: "repo",
		process.ScriptOnDone:  "user",
	}
	for typ, w := range want {
		got := st.scriptSources[typ]
		if got != w {
			t.Fatalf("scriptSources[%v] = %q, want %q (all: %v)", typ, got, w, st.scriptSources)
		}
	}
	rendered := renderWorkspaceStatus(st)
	if !strings.Contains(rendered, "run:         project") && !strings.Contains(rendered, "project") {
		t.Fatalf("render missing project source label:\n%s", rendered)
	}
}

// TestBuildWorkspaceStatus_Empties proves missing data renders sane empties:
// no port, no run, no scripts, no env — the dialog still opens.
func TestBuildWorkspaceStatus_Empties(t *testing.T) {
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}
	ws := &data.Workspace{Name: "bare", Repo: t.TempDir(), Root: t.TempDir(), Shelved: true}

	st := app.buildWorkspaceStatus(ws)
	rendered := renderWorkspaceStatus(st)
	for _, want := range []string{"shelved", "none allocated", "not running", "no repo config", "(none)"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("empty render missing %q:\n%s", want, rendered)
		}
	}
}

// TestHandleShowWorkspaceStatus_OpensDialog proves the message path opens
// the viewer with the composed snapshot.
func TestHandleShowWorkspaceStatus_OpensDialog(t *testing.T) {
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
		width:            120,
		height:           40,
	}
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}

	cmd := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the status open to return a fetch cmd")
	}
	ready, ok := cmd().(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("fetch cmd emitted %T, want workspaceStatusReadyMsg", cmd())
	}
	app.handleWorkspaceStatusReady(ready)

	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("status dialog did not open")
	}
	view := ansi.Strip(app.overlays.runOutput.View())
	for _, want := range []string{"identity", "runtime", "scripts", "env keys", "feat"} {
		if !strings.Contains(view, want) {
			t.Fatalf("status view missing %q:\n%s", want, view)
		}
	}
}

// durableStatusApp wires the production chain for status tests: a durable
// reservation store under a temp state home, a runner on it, and the service
// the status dialog consults.
func durableStatusApp(t *testing.T, home string) (*App, *data.PortReservationStore) {
	t.Helper()
	store := data.NewPortReservationStore(home)
	if err := store.Initialize(nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	scripts := process.NewScriptRunner(6200, 10)
	scripts.SetPortReservationStore(store)
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		width:            120,
		height:           40,
	}
	return app, store
}

func savedStatusWorkspace(t *testing.T, meta, name string) *data.Workspace {
	t.Helper()
	store := data.NewWorkspaceStore(meta)
	ws := &data.Workspace{Name: name, Repo: t.TempDir(), Root: t.TempDir()}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save(%s): %v", name, err)
	}
	return ws
}

// statusInterval runs the real async fetch the open cmd performs and applies
// the result through the token-fenced handler — the same path production
// takes, minus the tmux run-status sweep (nil host → not running).
func statusInterval(t *testing.T, app *App, ws *data.Workspace) {
	t.Helper()
	cmd := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	if cmd == nil {
		t.Fatal("status open returned no fetch cmd")
	}
	ready, ok := cmd().(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("fetch cmd emitted %T, want workspaceStatusReadyMsg", cmd())
	}
	app.handleWorkspaceStatusReady(ready)
}

// TestWorkspaceStatusReady_PersistedIntervalAuthoritative proves the dialog
// renders the stored interval's actual end — an interval minted under an old
// configured width keeps its original bounds even though config changed.
func TestWorkspaceStatusReady_PersistedIntervalAuthoritative(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	app, store := durableStatusApp(t, home)
	ws := savedStatusWorkspace(t, meta, "ws")

	// Reserve under an older, wider configuration by writing the registry
	// directly — the width mismatch is exactly what config reconstruction
	// would have lied about.
	id, ok := ws.StoredID()
	if !ok {
		t.Fatal("fixture: workspace has no stored ID")
	}
	base, end, err := store.Reserve(string(id), 6200, 25)
	if err != nil {
		t.Fatal(err)
	}
	if base != 6200 || end != 6224 {
		t.Fatalf("seeded interval = %d-%d, want 6200-6224", base, end)
	}
	app.config.PortRangeSize = 4 // "current" config disagrees with the record

	statusInterval(t, app, ws)
	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("status dialog did not open")
	}
	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "6200-6224") {
		t.Fatalf("status shows wrong interval (want stored 6200-6224, not config-derived 6200-6203):\n%s", view)
	}
}

// TestWorkspaceStatusReady_RestartSeesForeignReservation proves a reservation
// committed by a previous/parallel instance renders without this process ever
// allocating — the registry, not the local map, is the source of truth.
func TestWorkspaceStatusReady_RestartSeesForeignReservation(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	app, store := durableStatusApp(t, home)
	ws := savedStatusWorkspace(t, meta, "ws")
	id, _ := ws.StoredID()

	// "Another instance" commits the reservation directly to the registry —
	// this runner's memory map never sees it.
	if _, _, err := store.Reserve(string(id), 6200, 10); err != nil {
		t.Fatal(err)
	}
	statusInterval(t, app, ws)

	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "6200-6209") {
		t.Fatalf("foreign reservation not visible in status:\n%s", view)
	}
	// The memory-only getter correctly reports this instance holds nothing.
	if _, held := app.workspaceService.WorkspaceScriptPort(ws); held {
		t.Fatal("WorkspaceScriptPort claims an allocation this instance never made")
	}
}

// TestWorkspaceStatusReady_AbsentReservationDoesNotAllocate proves viewing
// status never creates a reservation.
func TestWorkspaceStatusReady_AbsentReservationDoesNotAllocate(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	app, store := durableStatusApp(t, home)
	ws := savedStatusWorkspace(t, meta, "ws")
	id, _ := ws.StoredID()

	statusInterval(t, app, ws)
	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "none allocated") {
		t.Fatalf("absent reservation should render 'none allocated':\n%s", view)
	}
	if _, _, found, err := store.Lookup(string(id)); err != nil || found {
		t.Fatalf("status read allocated a reservation (found=%v, err=%v)", found, err)
	}
}

// TestWorkspaceStatusReady_UnsavedWorkspaceNoReservation proves an unsaved
// workspace (no durable key) renders "none allocated" rather than minting a
// path-keyed lie.
func TestWorkspaceStatusReady_UnsavedWorkspaceNoReservation(t *testing.T) {
	home := t.TempDir()
	app, _ := durableStatusApp(t, home)
	ws := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}

	statusInterval(t, app, ws)
	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "none allocated") {
		t.Fatalf("unsaved workspace should render 'none allocated':\n%s", view)
	}
}

// TestWorkspaceStatusReady_ReadFailureSurfaces proves a corrupt registry is a
// visible error, not a blank/guessed range.
func TestWorkspaceStatusReady_ReadFailureSurfaces(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	app, store := durableStatusApp(t, home)
	ws := savedStatusWorkspace(t, meta, "ws")
	if err := os.WriteFile(store.Path(), []byte(`{"version": 1, "reservations": {"a": {"start": 1, "end": 9}, "b": {"start": 5, "end": 20}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	statusInterval(t, app, ws)
	if app.overlays.runOutput != nil && app.overlays.runOutput.Visible() {
		t.Fatal("status dialog opened on top of a corrupt registry")
	}
	if !app.toast.Visible() {
		t.Fatal("port read failure produced no visible error")
	}
}

// TestWorkspaceStatusReady_StaleResultDropped proves the dialog token fences
// a status fetch that resolves after a newer dialog superseded it.
func TestWorkspaceStatusReady_StaleResultDropped(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	app, store := durableStatusApp(t, home)
	ws := savedStatusWorkspace(t, meta, "ws")
	id, _ := ws.StoredID()
	if _, _, err := store.Reserve(string(id), 6200, 10); err != nil {
		t.Fatal(err)
	}

	// First open captures token T; a second open bumps it. The first fetch's
	// result must be dropped — a stale snapshot must not replace the dialog.
	first := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	second := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	staleMsg, freshMsg := first(), second()
	stale, ok := staleMsg.(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("first fetch returned %T, want workspaceStatusReadyMsg", staleMsg)
	}
	fresh, ok := freshMsg.(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("second fetch returned %T, want workspaceStatusReadyMsg", freshMsg)
	}
	app.handleWorkspaceStatusReady(stale)
	if app.overlays.runOutput != nil && app.overlays.runOutput.Visible() {
		t.Fatal("stale status result opened a dialog")
	}
	app.handleWorkspaceStatusReady(fresh)
	if app.overlays.runOutput == nil || !app.overlays.runOutput.Visible() {
		t.Fatal("fresh status result did not open the dialog")
	}
}

// TestScriptsTrusted covers the new read side of the trust gate at the
// runner level (the app path exercises it through buildWorkspaceStatus).
func TestScriptsTrusted(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate the trust registry (~/.amux)
	repo := t.TempDir()
	scripts := process.NewScriptRunner(6200, 10)
	if trusted, err := scripts.ScriptsTrusted(repo); err != nil || !trusted {
		t.Fatalf("config-less repo: trusted=%v err=%v, want true/nil", trusted, err)
	}
	writeRepoConfig(t, repo, `{"run": "echo hi"}`)
	if trusted, _ := scripts.ScriptsTrusted(repo); trusted {
		t.Fatal("untrusted repo config reported trusted")
	}
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	if trusted, _ := scripts.ScriptsTrusted(repo); !trusted {
		t.Fatal("trusted repo config reported untrusted")
	}
}
