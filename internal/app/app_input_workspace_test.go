package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
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

func TestHandleDialogResultTrustScriptsTrustsAndRetriesSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "setup-ran")
	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+marker+`"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	scripts := process.NewScriptRunner(6200, 10)
	err := scripts.RunSetup(ws)
	var trustErr *process.ScriptsNotTrustedError
	if !errors.As(err, &trustErr) {
		t.Fatalf("expected initial setup to be blocked by trust gate, got %v", err)
	}
	app := &App{
		workspaceService:       newWorkspaceService(nil, nil, scripts, ""),
		dialog:                 common.NewConfirmDialog(DialogTrustScripts, "Trust", "Trust scripts?"),
		dialogWorkspace:        ws,
		dialogTrustScriptsHash: trustErr.ConfigHash,
	}

	cmd := app.handleDialogResult(common.DialogResult{ID: DialogTrustScripts, Confirmed: true})
	if cmd == nil {
		t.Fatal("expected trust confirmation to return a setup retry command")
	}
	msg, ok := cmd().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	if msg.Err != nil {
		t.Fatalf("trust-and-retry setup failed: %v", msg.Err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected trusted setup command to run: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if err := scripts.RunSetup(ws); err != nil {
		t.Fatalf("expected repo config to remain trusted after dialog confirmation: %v", err)
	}
}

func TestTrustScriptsRetryRejectsChangedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	originalMarker := filepath.Join(wsRoot, "original")
	changedMarker := filepath.Join(wsRoot, "changed")
	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+originalMarker+`"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	scripts := process.NewScriptRunner(6200, 10)
	err := scripts.RunSetup(ws)
	var trustErr *process.ScriptsNotTrustedError
	if !errors.As(err, &trustErr) {
		t.Fatalf("expected initial setup to be blocked by trust gate, got %v", err)
	}

	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+changedMarker+`"]}`)
	service := newWorkspaceService(nil, nil, scripts, "")
	msg, ok := service.TrustRepoScriptsAndRunSetupAsync(ws, trustErr.ConfigHash)().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	var changedTrustErr *process.ScriptsNotTrustedError
	if !errors.As(msg.Err, &changedTrustErr) {
		t.Fatalf("expected changed config to be re-gated with a trust error, got %v", msg.Err)
	}
	if changedTrustErr.ConfigHash == trustErr.ConfigHash {
		t.Fatal("expected changed config trust prompt to carry a new hash")
	}
	if _, err := os.Stat(originalMarker); !os.IsNotExist(err) {
		t.Fatalf("original setup command should not run after stale approval, stat err=%v", err)
	}
	if _, err := os.Stat(changedMarker); !os.IsNotExist(err) {
		t.Fatalf("changed setup command should not run under stale approval, stat err=%v", err)
	}
}

// newTrustDialogApp builds a minimal App whose workspaceService can load a
// repo's .amux/workspaces.json, so the trust-dialog show handler can compute its
// indirection warning. config is non-nil because presentDialog reads UI hints.
func newTrustDialogApp() *App {
	return &App{
		config:           &config.Config{},
		width:            120,
		height:           40,
		workspaceService: newWorkspaceService(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}
}

// TestHandleShowTrustScriptsDialog_WarnsOnInRepoScriptReference proves the trust
// dialog surfaces in-repo files an approved command reaches into (via the
// already-shipped process.ReferencesInRepoFiles detector), so the user knows the
// manifest hash they approve does not cover those scripts' later contents. This
// is informational only and changes nothing the trust gate blocks.
func TestHandleShowTrustScriptsDialog_WarnsOnInRepoScriptReference(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "dev.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write dev.sh: %v", err)
	}
	workspaceSetupConfig(t, repo, `{"setup-workspace":["bash ./scripts/dev.sh"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if !strings.Contains(view, "scripts/dev.sh") {
		t.Fatalf("expected trust dialog to name the referenced in-repo script, got:\n%s", view)
	}
	if !strings.Contains(view, "re-verify") {
		t.Fatalf("expected the indirection warning wording, got:\n%s", view)
	}
}

// TestHandleShowTrustScriptsDialog_WarnsOnUnresolvableCommand proves a command
// with expansion/glob constructs (which hide references from the static
// detector) raises the "amux can't list every file" warning.
func TestHandleShowTrustScriptsDialog_WarnsOnUnresolvableCommand(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"run":"$MYVAR/foo"}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if !strings.Contains(view, "variables/globs") {
		t.Fatalf("expected unresolvable-command warning, got:\n%s", view)
	}
}

// TestHandleShowTrustScriptsDialog_NoIndirectionMakesNoSafetyClaim proves that
// when the detector finds neither a referenced file nor an unresolvable
// construct, the dialog renders no extra warning AND no reassuring "safe" text:
// an empty detector result is explicitly NOT a safety guarantee.
func TestHandleShowTrustScriptsDialog_NoIndirectionMakesNoSafetyClaim(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"setup-workspace":["echo hello"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if strings.Contains(view, "re-verify") || strings.Contains(view, "variables/globs") {
		t.Fatalf("expected no indirection warning for a plain command, got:\n%s", view)
	}
	for _, banned := range []string{"safe", "no in-repo", "nothing to run", "verified"} {
		if strings.Contains(strings.ToLower(view), banned) {
			t.Fatalf("dialog must not imply safety (%q), got:\n%s", banned, view)
		}
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
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete success")
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
	// RunSetup builds the env (and thus allocates the port range) even with no
	// configured setup commands, mirroring a workspace that has run scripts.
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
		workspaceService: newWorkspaceService(nil, nil, scripts, ""),
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

func TestSyncActiveWorkspacesToDashboard_SkipsDeleteInFlight(t *testing.T) {
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
	app.markWorkspaceDeleteInFlight(wsA, true)
	app.syncActiveWorkspacesToDashboard()

	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 1 {
		t.Fatalf("expected 1 active workspace (delete-in-flight wsA excluded), got %d", got)
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

	app.markWorkspaceDeleteInFlight(ws, true)
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
			phases: map[string]lifecyclePhase{idDel: lifecycleDeleting},
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
		workspaceService: newWorkspaceService(
			nil,
			nil,
			nil,
			"",
		),
		activeWorkspace: wsDel,
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
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
	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker cleared")
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
