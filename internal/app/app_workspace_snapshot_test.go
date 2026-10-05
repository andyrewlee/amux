package app

import (
	"strconv"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// Plan 035: a background Cmd that captures the live *data.Workspace reads
// Scripts/Env/Repo/Root while the Update loop mutates the same fields —
// torn state at best, a map data race at worst. Every Cmd built from a
// workspace must capture a Clone made on the Update goroutine. These tests
// pin that the workspace a dispatched Cmd hands to the service (and embeds
// in its result) is a snapshot, never the live pointer.

func snapshotFixtureWs() *data.Workspace {
	return &data.Workspace{
		Name:    "feature",
		Repo:    "/repo",
		Root:    "/repo/ws",
		Branch:  "feature",
		Base:    "main",
		Scripts: data.ScriptsConfig{Run: "make dev"},
		Env:     map[string]string{"K": "v"},
	}
}

func assertSnapshotNotLive(t *testing.T, got, live *data.Workspace, site string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: result carried a nil workspace", site)
	}
	if got == live {
		t.Fatalf("%s: cmd captured the live workspace pointer — off-loop reads race Update-loop writes", site)
	}
	if got.ID() != live.ID() {
		t.Fatalf("%s: snapshot lost record identity (ID %q vs %q)", site, got.ID(), live.ID())
	}
}

// TestBackgroundCmds_CaptureWorkspaceSnapshot asserts the clone boundary at
// every app dispatch site that hands a workspace to an async Cmd: the
// workspace embedded in the produced message is a Clone, never the caller's
// live pointer.
func TestBackgroundCmds_CaptureWorkspaceSnapshot(t *testing.T) {
	project := &data.Project{Name: "repo", Path: "/repo"}

	newSvc := func(t *testing.T) *workspacesvc.Service {
		t.Helper()
		return workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), process.NewScriptRunner(6200, 10), t.TempDir())
	}

	t.Run("run-script status check", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{sidebar: sidebar.NewTabbedSidebar(), activeWorkspace: ws, workspaceService: newSvc(t)}
		cmd := app.requestRunScriptStatus()
		if cmd == nil {
			t.Fatal("requestRunScriptStatus returned nil")
		}
		result, ok := cmd().(messages.RunScriptStatusResult)
		if !ok {
			t.Fatalf("cmd produced %T, want RunScriptStatusResult", result)
		}
		assertSnapshotNotLive(t, result.Workspace, ws, "requestRunScriptStatus")
	})

	t.Run("run-output session enumeration", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{workspaceService: newSvc(t)}
		cmd := app.handleShowRunScriptOutput(messages.ShowRunScriptOutput{Workspace: ws})
		if cmd == nil {
			t.Fatal("handleShowRunScriptOutput returned nil")
		}
		result, ok := cmd().(runSessionsEnumeratedMsg)
		if !ok {
			t.Fatalf("cmd produced %T, want runSessionsEnumeratedMsg", result)
		}
		assertSnapshotNotLive(t, result.ws, ws, "handleShowRunScriptOutput")
	})

	t.Run("run-viewer attach target", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{workspaceService: newSvc(t)}
		cmd := app.attachRunViewerCmd(ws, "")
		if cmd == nil {
			t.Fatal("attachRunViewerCmd returned nil")
		}
		result, ok := cmd().(runAttachTargetMsg)
		if !ok {
			t.Fatalf("cmd produced %T, want runAttachTargetMsg", result)
		}
		assertSnapshotNotLive(t, result.ws, ws, "attachRunViewerCmd")
	})

	t.Run("run-script toggle", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{workspaceService: newSvc(t)}
		cmd := app.handleToggleWorkspaceScript(messages.ToggleWorkspaceScript{Workspace: ws})
		if cmd == nil {
			t.Fatal("handleToggleWorkspaceScript returned nil")
		}
		result, ok := cmd().(messages.WorkspaceScriptStateChanged)
		if !ok {
			t.Fatalf("cmd produced %T, want WorkspaceScriptStateChanged", result)
		}
		assertSnapshotNotLive(t, result.Workspace, ws, "handleToggleWorkspaceScript")
	})

	t.Run("setup rerun", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{workspaceService: workspacesvc.New(nil, nil, nil, t.TempDir())}
		cmd := app.handleRerunWorkspaceScript(messages.RerunWorkspaceScript{Workspace: ws, Script: process.ScriptSetup})
		if cmd == nil {
			t.Fatal("handleRerunWorkspaceScript returned nil")
		}
		result, ok := cmd().(messages.WorkspaceSetupComplete)
		if !ok {
			t.Fatalf("cmd produced %T, want WorkspaceSetupComplete", result)
		}
		assertSnapshotNotLive(t, result.Workspace, ws, "handleRerunWorkspaceScript")
	})

	t.Run("merge precondition", func(t *testing.T) {
		ws := snapshotFixtureWs()
		app := &App{
			localBaseBranchFn:  func(_, _ string) string { return "main" },
			checkedOutBranchFn: func(_ string) (string, error) { return "other", nil },
		}
		cmd := app.resolveMergePreconditionAsync(ws)
		if cmd == nil {
			t.Fatal("resolveMergePreconditionAsync returned nil")
		}
		result, ok := cmd().(messages.MergeWorkspaceRefused)
		if !ok {
			t.Fatalf("cmd produced %T, want MergeWorkspaceRefused", result)
		}
		assertSnapshotNotLive(t, result.Workspace, ws, "resolveMergePreconditionAsync")
	})

	t.Run("lifecycle ops", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			disp func(a *App, ws *data.Workspace) tea.Cmd
		}{
			{"shelve", func(a *App, ws *data.Workspace) tea.Cmd { return a.shelveWorkspace(project, ws) }},
			{"restore", func(a *App, ws *data.Workspace) tea.Cmd { return a.restoreWorkspace(project, ws) }},
			{"delete", func(a *App, ws *data.Workspace) tea.Cmd { return a.deleteWorkspace(project, ws) }},
			{"setup", func(a *App, ws *data.Workspace) tea.Cmd { return a.runSetupAsync(ws) }},
			{"trust+setup", func(a *App, ws *data.Workspace) tea.Cmd { return a.trustRepoScriptsAndRunSetupAsync(ws, "hash") }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ws := snapshotFixtureWs()
				app := &App{workspaceService: workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), nil, t.TempDir())}
				cmd := tc.disp(app, ws)
				if cmd == nil {
					t.Fatalf("%s returned nil cmd", tc.name)
				}
				result := cmd()
				got := workspaceFromMsg(t, result, tc.name)
				assertSnapshotNotLive(t, got, ws, tc.name)
			})
		}
	})
}

// workspaceFromMsg extracts the carried workspace from whichever lifecycle
// result a dispatched op produced — success or typed failure both embed it.
func workspaceFromMsg(t *testing.T, msg tea.Msg, site string) *data.Workspace {
	t.Helper()
	switch m := msg.(type) {
	case messages.WorkspaceShelved:
		return m.Workspace
	case messages.WorkspaceShelveFailed:
		return m.Workspace
	case messages.WorkspaceRestored:
		return m.Workspace
	case messages.WorkspaceRestoreFailed:
		return m.Workspace
	case messages.WorkspaceRestoreSkipped:
		return m.Workspace
	case messages.WorkspaceDeleted:
		return m.Workspace
	case messages.WorkspaceDeleteFailed:
		return m.Workspace
	case messages.WorkspaceSetupComplete:
		return m.Workspace
	default:
		t.Fatalf("%s: unhandled result type %T — add it to workspaceFromMsg", site, msg)
		return nil
	}
}

// TestRunScriptStatusCmd_ImmutableEnv proves the snapshot boundary carries
// real data semantics: an Env edit that lands after dispatch must not leak
// into the workspace the cmd hands to the service.
func TestRunScriptStatusCmd_ImmutableEnv(t *testing.T) {
	ws := snapshotFixtureWs()
	app := &App{
		sidebar:          sidebar.NewTabbedSidebar(),
		activeWorkspace:  ws,
		workspaceService: workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), process.NewScriptRunner(6200, 10), t.TempDir()),
	}
	cmd := app.requestRunScriptStatus()
	if cmd == nil {
		t.Fatal("requestRunScriptStatus returned nil")
	}
	// Dispatch is synchronous — the clone is taken now; writes after this
	// point must be invisible to the snapshot.
	ws.Env["MARKER"] = "written-after-dispatch"
	ws.Scripts.Run = "changed-after-dispatch"
	result, ok := cmd().(messages.RunScriptStatusResult)
	if !ok {
		t.Fatalf("cmd produced %T", result)
	}
	if _, leaked := result.Workspace.Env["MARKER"]; leaked {
		t.Fatal("a post-dispatch Env write leaked into the snapshot — the cmd still reads the live map")
	}
	if result.Workspace.Scripts.Run == "changed-after-dispatch" {
		t.Fatal("a post-dispatch Scripts write leaked into the snapshot")
	}
	if result.Workspace.Scripts.Run != "make dev" {
		t.Fatalf("snapshot lost the dispatch-time value: Run=%q", result.Workspace.Scripts.Run)
	}
}

// TestRunScriptStatusCmd_NoSharedReadsUnderMutation is the detector-level
// proof: a writer hammering the live workspace's mutable fields while the
// status cmd executes. Pre-fix the cmd reads ws.Scripts/ws.Root through the
// shared pointer and -race reports the overlap; post-fix it reads its own
// clone and the writer's noise is invisible.
func TestRunScriptStatusCmd_NoSharedReadsUnderMutation(t *testing.T) {
	ws := snapshotFixtureWs()
	runner := process.NewScriptRunner(6200, 10)
	runner.SetRunHost(&stubRunSessionHost{})
	app := &App{
		sidebar:          sidebar.NewTabbedSidebar(),
		activeWorkspace:  ws,
		workspaceService: workspacesvc.New(nil, data.NewWorkspaceStore(t.TempDir()), runner, t.TempDir()),
	}
	cmd := app.requestRunScriptStatus()
	if cmd == nil {
		t.Fatal("requestRunScriptStatus returned nil")
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				s := strconv.Itoa(i)
				ws.Scripts.Run = s
				ws.Env["K"] = s
			}
		}
	}()
	result, ok := cmd().(messages.RunScriptStatusResult)
	close(stop)
	wg.Wait()
	if !ok {
		t.Fatalf("cmd produced %T", result)
	}
	if result.Workspace == ws {
		t.Fatal("cmd captured the live workspace pointer")
	}
	if result.Workspace.Env["K"] == "" {
		t.Fatal("snapshot lost the dispatch-time env")
	}
}
