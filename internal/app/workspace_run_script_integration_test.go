package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestRunScriptToggle_EndToEnd drives the whole run-script chain against a real
// process: the dispatch case routes ToggleWorkspaceScript, the service spawns
// the repo's `run` command, and the resulting state change reaches the sidebar
// indicator. The unit tests above each cover one hop; this asserts the hops are
// actually connected, which is the defect DIR-01 existed to fix — every piece
// was built and nothing invoked them.
func TestRunScriptToggle_EndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(tmp, "ws")
	for _, dir := range []string{filepath.Join(repo, ".amux"), wsRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	marker := filepath.Join(wsRoot, "run-marker.txt")
	config := `{"run": "touch ` + marker + `; sleep 300"}`
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"), []byte(config), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}

	scripts := process.NewScriptRunner(6800, 10)
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	t.Cleanup(scripts.StopAll)

	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)
	sb := sidebar.NewTabbedSidebar()
	sb.SetSize(60, 20)
	sb.SetWorkspace(ws)
	sb.SetGitStatus(&git.StatusResult{Clean: true})

	app := &App{
		toast:            common.NewToastModel(),
		sidebar:          sb,
		activeWorkspace:  ws,
		workspaceService: newWorkspaceService(nil, nil, scripts, filepath.Join(tmp, "managed")),
	}

	// --- Start: route the toggle exactly as Update would. ---
	var cmds []tea.Cmd
	if !app.updateWorkspaceLifecycleMsg(messages.ToggleWorkspaceScript{Workspace: ws}, &cmds) {
		t.Fatal("ToggleWorkspaceScript is not routed by the workspace-lifecycle dispatch")
	}
	if len(cmds) != 1 {
		t.Fatalf("dispatch produced %d commands, want 1", len(cmds))
	}

	started, ok := cmds[0]().(messages.WorkspaceScriptStateChanged)
	if !ok {
		t.Fatalf("toggle produced %T, want WorkspaceScriptStateChanged", cmds[0]())
	}
	if started.Err != nil {
		t.Fatalf("starting the run script failed: %v", started.Err)
	}
	if !started.Running {
		t.Fatal("toggle reported the script as not running after a successful start")
	}
	app.handleWorkspaceScriptStateChanged(started)

	if !scripts.IsRunning(ws) {
		t.Fatal("the run script is not tracked as running after the start toggle")
	}
	if !runIndicatorVisible(sb) {
		t.Fatal("the sidebar shows no [run] indicator while the script is running")
	}
	waitForFile(t, marker)

	// --- Stop: the same key must tear it back down. ---
	cmds = nil
	if !app.updateWorkspaceLifecycleMsg(messages.ToggleWorkspaceScript{Workspace: ws}, &cmds) {
		t.Fatal("the second ToggleWorkspaceScript was not routed")
	}
	stopped, ok := cmds[0]().(messages.WorkspaceScriptStateChanged)
	if !ok {
		t.Fatalf("second toggle produced %T, want WorkspaceScriptStateChanged", cmds[0]())
	}
	if stopped.Err != nil {
		t.Fatalf("stopping the run script failed: %v", stopped.Err)
	}
	if stopped.Running {
		t.Fatal("the second toggle did not stop the script")
	}
	app.handleWorkspaceScriptStateChanged(stopped)

	if scripts.IsRunning(ws) {
		t.Fatal("the run script is still tracked as running after the stop toggle")
	}
	if runIndicatorVisible(sb) {
		t.Fatal("the sidebar still shows [run] after the script was stopped")
	}
}

// TestRunScriptIndicatorClearsWhenTheScriptExitsOnItsOwn covers the case
// neither start nor stop reports: a run script that ends by itself — a dev
// server that crashes, or a command that simply finishes. Nothing pushes that
// to the UI, so without the periodic reconcile the [run] marker would claim a
// live script forever.
func TestRunScriptIndicatorClearsWhenTheScriptExitsOnItsOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(tmp, "ws")
	for _, dir := range []string{filepath.Join(repo, ".amux"), wsRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	// A script that exits promptly on its own.
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"),
		[]byte(`{"run": "true"}`), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}

	scripts := process.NewScriptRunner(6900, 10)
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	t.Cleanup(scripts.StopAll)

	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)
	sb := sidebar.NewTabbedSidebar()
	sb.SetSize(60, 20)
	sb.SetWorkspace(ws)
	sb.SetGitStatus(&git.StatusResult{Clean: true})

	app := &App{
		toast:            common.NewToastModel(),
		sidebar:          sb,
		activeWorkspace:  ws,
		workspaceService: newWorkspaceService(nil, nil, scripts, filepath.Join(tmp, "managed")),
	}

	started, ok := app.workspaceService.ToggleScriptAsync(ws)().(messages.WorkspaceScriptStateChanged)
	if !ok || started.Err != nil {
		t.Fatalf("starting the run script failed: %+v", started)
	}
	app.handleWorkspaceScriptStateChanged(started)
	if !runIndicatorVisible(sb) {
		t.Fatal("the indicator did not appear when the script started")
	}

	// Let the script exit by itself; nothing notifies the app.
	deadline := time.Now().Add(5 * time.Second)
	for scripts.IsRunning(ws) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if scripts.IsRunning(ws) {
		t.Fatal("setup: the run script never exited on its own")
	}
	if !runIndicatorVisible(sb) {
		t.Fatal("setup: the indicator cleared without a reconcile, so this test proves nothing")
	}

	app.syncRunScriptIndicator()
	if runIndicatorVisible(sb) {
		t.Fatal("the [run] indicator survived a script that exited on its own")
	}
}

// waitForFile polls for path, giving a spawned process a moment to create it.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the run script never created %s; it did not actually execute", path)
}
