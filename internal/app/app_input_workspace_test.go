package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func workspaceSetupConfig(t *testing.T, repoPath, content string) {
	t.Helper()
	configDir := filepath.Join(repoPath, ".amux")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

func runCommandMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	// Flatten nested batches (e.g. a handler batching a ReportError cmd, which
	// is itself a batch) so callers see the leaf messages.
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, batchCmd := range batch {
			msgs = append(msgs, runCommandMessages(batchCmd)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

// TestHandleWorkspaceSetupComplete_TrustSkipToast proves the setup-complete
// handler distinguishes a trust skip (ErrScriptsNotTrusted) from a generic
// setup failure, naming .amux/workspaces.json so the user knows what was
// skipped and why, and prompts the user to trust the current repo config.
func TestHandleWorkspaceSetupComplete_TrustSkipToastAndDialog(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	app := &App{toast: common.NewToastModel()}

	wrapped := &process.ScriptsNotTrustedError{
		Repo:       "/repo",
		Command:    "touch marker",
		ConfigHash: "abc123",
	}
	cmd := app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
		Workspace: ws,
		Err:       wrapped,
	})
	if cmd == nil {
		t.Fatal("expected a warning toast command for a trust-skip error")
	}

	view := app.toast.View()
	if !strings.Contains(view, ".amux/workspaces.json") {
		t.Fatalf("trust-skip toast should name .amux/workspaces.json, got: %q", view)
	}
	if strings.Contains(view, "Setup failed") {
		t.Fatalf("trust-skip toast must not use the generic 'Setup failed' wording, got: %q", view)
	}
	var foundTrustPrompt bool
	for _, msg := range runCommandMessages(cmd) {
		prompt, ok := msg.(messages.ShowTrustScriptsDialog)
		if ok && prompt.Workspace == ws {
			if prompt.ConfigHash != "abc123" {
				t.Fatalf("trust prompt hash = %q, want abc123", prompt.ConfigHash)
			}
			foundTrustPrompt = true
		}
	}
	if !foundTrustPrompt {
		t.Fatal("expected trust-skip command to open the repo script trust dialog")
	}
}

// TestHandleWorkspaceSetupComplete_GenericFailureRoutesThroughReportError proves
// non-trust setup failures route through common.ReportError: the returned batch
// emits a logged messages.Error and an error-level toast whose text keeps the
// unchanged generic "Setup failed" wording.
func TestHandleWorkspaceSetupComplete_GenericFailureRoutesThroughReportError(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	app := &App{toast: common.NewToastModel()}

	cmd := app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
		Workspace: ws,
		Err:       errors.New("boom"),
	})
	if cmd == nil {
		t.Fatal("expected an error-report command for a generic setup failure")
	}

	var sawLoggedError, sawFailureToast bool
	for _, msg := range runCommandMessages(cmd) {
		switch m := msg.(type) {
		case messages.Error:
			if !m.Logged {
				t.Error("expected ReportError to mark the emitted messages.Error as Logged")
			}
			if m.Err == nil {
				t.Error("expected emitted messages.Error to carry the underlying error")
			}
			sawLoggedError = true
		case messages.Toast:
			if m.Level != messages.ToastError {
				t.Errorf("setup-failure toast level = %v, want ToastError", m.Level)
			}
			if !strings.Contains(m.Message, "Setup failed") {
				t.Errorf("setup-failure toast should say 'Setup failed', got: %q", m.Message)
			}
			sawFailureToast = true
		}
	}
	if !sawLoggedError {
		t.Fatal("expected a logged messages.Error from the setup-failure path")
	}
	if !sawFailureToast {
		t.Fatal("expected an error-level 'Setup failed' toast from the setup-failure path")
	}
}

func TestHandleWorkspaceDeletedClearsDirtyWorkspaceMarker(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: map[string]lifecyclePhase{wsID: lifecycleMutating},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected mutation-in-flight marker to be cleared on delete success")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty workspace marker to be cleared on delete success")
	}
}

// TestHandleWorkspaceDeleted_ReleasesPortAllocation proves the confirmed-delete
// path releases the workspace's port allocation through the workspace service so
// the allocator's map does not retain an entry per deleted workspace.
func TestHandleWorkspaceDeleted_ReleasesPortAllocation(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	scripts := process.NewScriptRunner(6200, 10)
	// RunSetup builds the env (and thus allocates the port range); a trivial
	// user-entered setup command keeps this at one fast no-op process.
	ws.Scripts.Setup = "true"
	if err := scripts.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if _, ok := scripts.PortAllocated(ws); !ok {
		t.Fatalf("expected port to be allocated after RunSetup")
	}

	app := &App{
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{},
			phases: map[string]lifecyclePhase{},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if _, ok := scripts.PortAllocated(ws); ok {
		t.Fatal("expected port allocation released on workspace delete")
	}
}

func TestSyncActiveWorkspacesToDashboard_SkipsMutationInFlight(t *testing.T) {
	wsA := &data.Workspace{Repo: "/repo", Root: "/repo/a"}
	wsB := &data.Workspace{Repo: "/repo", Root: "/repo/b"}
	idA, idB := string(wsA.ID()), string(wsB.ID())

	app := &App{
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{idA: true, idB: true},
		},
		dashboard: dashboard.New(),
	}
	app.markWorkspaceMutationInFlight(wsA, true)
	app.syncActiveWorkspacesToDashboard()

	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 1 {
		t.Fatalf("expected 1 active workspace (mutation-in-flight wsA excluded), got %d", got)
	}
}

func TestHandleWorkspaceDeleteFailedRequestsFreshActivityScan(t *testing.T) {
	ws := &data.Workspace{Repo: "/repo", Root: "/repo/a"}
	wsID := string(ws.ID())
	app := &App{
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{wsID: true},
		},
		tmuxAvailable: true,
		dashboard:     dashboard.New(),
	}

	app.markWorkspaceMutationInFlight(ws, true)
	app.syncActiveWorkspacesToDashboard()
	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 0 {
		t.Fatalf("expected active workspace to be filtered during delete, got %d", got)
	}

	app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{
		Workspace: ws,
		Err:       errors.New("delete failed"),
	})
	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 0 {
		t.Fatalf("expected cached active state to stay filtered until fresh scan, got %d", got)
	}
	if !app.tmuxActivity.scanInFlight {
		t.Fatal("expected failed delete to request a fresh tmux activity scan")
	}
}

func TestHandleWorkspaceDeleted_ClearsActiveWorkspace(t *testing.T) {
	wsDel := data.NewWorkspace("del", "del", "main", "/repo", "/repo/del")
	wsKeep := data.NewWorkspace("keep", "keep", "main", "/repo", "/repo/keep")
	idDel, idKeep := string(wsDel.ID()), string(wsKeep.ID())

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{idDel: true, idKeep: true},
		},
		lifecycle: workspaceLifecycleState{
			phases: map[string]lifecyclePhase{idDel: lifecycleMutating},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: wsDel})

	if app.tmuxActivity.activeWorkspaceIDs[idDel] {
		t.Fatal("expected deleted workspace cleared from the active set")
	}
	if !app.tmuxActivity.activeWorkspaceIDs[idKeep] {
		t.Fatal("expected surviving workspace to remain in the active set")
	}
}

func TestHandleWorkspaceDeleted_WithMetadataErrorRemovesLoadedWorkspace(t *testing.T) {
	wsDel := data.NewWorkspace("del", "del", "main", "/repo", "/repo/del")
	wsKeep := data.NewWorkspace("keep", "keep", "main", "/repo", "/repo/keep")
	project := data.NewProject("/repo")
	project.Workspaces = []data.Workspace{*wsDel, *wsKeep}
	wsID := string(wsDel.ID())

	app := &App{
		projects:        []data.Project{*project},
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		workspaceService: workspacesvc.New(
			nil,
			nil,
			nil,
			"",
		),
		activeWorkspace: wsDel,
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: map[string]lifecyclePhase{wsID: lifecycleMutating},
		},
	}
	app.dashboard.SetProjects(app.projects)

	cmds := app.handleWorkspaceDeleted(messages.WorkspaceDeleted{
		Workspace: wsDel,
		Err:       errors.New("metadata delete failed"),
	})

	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected deleted workspace removed from loaded projects, got %+v", app.projects)
	}
	if got := app.projects[0].Workspaces[0].Root; got != wsKeep.Root {
		t.Fatalf("expected surviving workspace %q, got %q", wsKeep.Root, got)
	}
	if app.activeWorkspace != nil {
		t.Fatal("expected metadata-error delete to still navigate away from deleted workspace")
	}
	if app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected mutation-in-flight marker cleared")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty marker cleared")
	}
	if len(cmds) == 0 {
		t.Fatal("expected metadata error to be reported")
	}

	staleProject := data.NewProject("/repo")
	staleProject.Workspaces = []data.Workspace{*wsDel, *wsKeep}
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*staleProject}})
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected stale reload to keep deleted workspace hidden, got %+v", app.projects)
	}
	if got := app.projects[0].Workspaces[0].Root; got != wsKeep.Root {
		t.Fatalf("expected stale reload survivor %q, got %q", wsKeep.Root, got)
	}
}
