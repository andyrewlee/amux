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
	if !st.portAllocated || st.portBase != 6200 || st.portEnd != 6209 {
		t.Fatalf("port = %d-%d allocated=%v, want 6200-6209 true", st.portBase, st.portEnd, st.portAllocated)
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
