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
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newScriptsTestHarness mirrors newEnvTestHarness: a harness App wired to a
// real temp-dir-backed WorkspaceStore with ws already saved in it.
func newScriptsTestHarness(t *testing.T, ws *data.Workspace) (*Harness, *data.WorkspaceStore, data.WorkspaceID) {
	t.Helper()
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	h.app.workspaceService = workspacesvc.New(nil, store, nil, "")
	return h, store, ws.ID()
}

func TestHandleShowWorkspaceScriptsDialog_SeedsDialog(t *testing.T) {
	ws := &data.Workspace{
		Name:       "feature",
		Repo:       "/repo/primary",
		Root:       "/repo/primary/ws",
		Scripts:    data.ScriptsConfig{Setup: "make deps", Run: "npm run dev"},
		ScriptMode: "concurrent",
	}
	h, _, _ := newScriptsTestHarness(t, ws)

	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})

	if h.app.overlays.scripts == nil || !h.app.overlays.scripts.Visible() {
		t.Fatal("expected scriptsDialog to be shown")
	}
	if h.app.overlays.scriptsWorkspace != ws {
		t.Fatalf("scriptsDialogWorkspace = %#v, want %#v", h.app.overlays.scriptsWorkspace, ws)
	}
	setup, run, archive, onDone, mode := h.app.overlays.scripts.Values()
	if setup != "make deps" || run != "npm run dev" || archive != "" || onDone != "" || mode != "concurrent" {
		t.Fatalf("dialog seeded (%q,%q,%q,%q,%q)", setup, run, archive, onDone, mode)
	}
}

func TestHandleShowWorkspaceScriptsDialog_NilWorkspaceIsNoop(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: nil})
	if h.app.overlays.scripts != nil {
		t.Fatal("expected no dialog for a nil workspace")
	}
}

func TestHandleScriptsDialogResult_PersistsAndUpdatesActiveWorkspace(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
	}
	h, store, id := newScriptsTestHarness(t, ws)
	h.app.activeWorkspace = ws

	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})
	// Cursor starts on setup; type a command, then Enter to confirm.
	h.app.overlays.scripts.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	h.app.overlays.scripts.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	cmd := h.app.handleScriptsDialogResult(common.ScriptsDialogResult{})
	if cmd == nil {
		t.Fatal("expected a success-toast cmd")
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after confirm error = %v", err)
	}
	if reloaded.Scripts.Setup != "ma" {
		t.Fatalf("persisted Scripts.Setup = %q, want %q", reloaded.Scripts.Setup, "ma")
	}
	if reloaded.ScriptMode != "nonconcurrent" {
		t.Fatalf("persisted ScriptMode = %q, want nonconcurrent", reloaded.ScriptMode)
	}
	if h.app.activeWorkspace.Scripts.Setup != "ma" {
		t.Fatalf("active workspace Scripts not updated in place: %#v", h.app.activeWorkspace.Scripts)
	}
	if h.app.overlays.scripts != nil || h.app.overlays.scriptsWorkspace != nil {
		t.Fatal("expected scriptsDialog/scriptsDialogWorkspace cleared after confirm")
	}
	if !strings.Contains(h.app.toast.View(), "feature") {
		t.Fatalf("expected a success toast naming the workspace, got %q", h.app.toast.View())
	}
}

// TestHandleRerunWorkspaceScript_TagsCompletion proves the re-run trigger
// drives the real setup path end to end: the command runs (marker file) and
// the completion arrives tagged Rerun so the handler can confirm it.
func TestHandleRerunWorkspaceScript_TagsCompletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "setup-ran")
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)
	ws.Scripts.Setup = "touch " + marker

	app := &App{
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}
	cmd := app.handleRerunWorkspaceScript(messages.RerunWorkspaceScript{
		Workspace: ws,
		Script:    process.ScriptSetup,
	})
	if cmd == nil {
		t.Fatal("expected a setup cmd for a user-entered setup script")
	}
	msg, ok := cmd().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("rerun cmd emitted %T, want messages.WorkspaceSetupComplete", cmd())
	}
	if msg.Err != nil {
		t.Fatalf("rerun setup failed: %v", msg.Err)
	}
	if !msg.Rerun {
		t.Fatal("completion must carry Rerun so the handler confirms it")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("user-entered setup command did not run: %v", err)
	}
}

// TestHandleRerunWorkspaceScript_RejectsNonSetup guards the handler's script
// whitelist: archive is a destructive teardown hook and stays non-triggerable.
func TestHandleRerunWorkspaceScript_RejectsNonSetup(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", t.TempDir())
	app := &App{workspaceService: workspacesvc.New(nil, nil, nil, "")}

	if cmd := app.handleRerunWorkspaceScript(messages.RerunWorkspaceScript{
		Workspace: ws,
		Script:    process.ScriptArchive,
	}); cmd != nil {
		t.Fatal("archive re-run must not be wired to a key")
	}
}

// TestHandleRerunWorkspaceScript_UntrustedRepoRegates pins the trust path: a
// re-run over an untrusted .amux/workspaces.json surfaces the same
// ErrScriptsNotTrusted the initial run does (the handler turns it into the
// trust dialog), never an execution.
func TestHandleRerunWorkspaceScript_UntrustedRepoRegates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "should-not-exist")
	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+marker+`"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	app := &App{
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}
	msg, ok := app.handleRerunWorkspaceScript(messages.RerunWorkspaceScript{
		Workspace: ws,
		Script:    process.ScriptSetup,
	})().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatal("expected WorkspaceSetupComplete")
	}
	if !errors.Is(msg.Err, process.ErrScriptsNotTrusted) {
		t.Fatalf("untrusted rerun error = %v, want ErrScriptsNotTrusted", msg.Err)
	}
	if !msg.Rerun {
		t.Fatal("even a gated completion keeps the Rerun tag")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("untrusted setup must not execute")
	}
}

// TestHandleWorkspaceSetupComplete_RerunToasts covers the three re-run
// outcomes: success confirms, nothing-configured explains, and an initial
// (non-rerun) run of either stays silent.
func TestHandleWorkspaceSetupComplete_RerunToasts(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")

	t.Run("rerun success confirms", func(t *testing.T) {
		app := &App{toast: common.NewToastModel()}
		_ = app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{Workspace: ws, Rerun: true})
		if !strings.Contains(app.toast.View(), "Setup completed") {
			t.Fatalf("expected a 'Setup completed' toast, got %q", app.toast.View())
		}
	})

	t.Run("rerun with no setup configured explains", func(t *testing.T) {
		app := &App{toast: common.NewToastModel()}
		_ = app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
			Workspace: ws, Rerun: true, Err: process.ErrNoScriptConfigured,
		})
		if !strings.Contains(app.toast.View(), "No setup script configured") {
			t.Fatalf("expected a 'no setup configured' toast, got %q", app.toast.View())
		}
	})

	t.Run("initial run with no setup stays silent", func(t *testing.T) {
		app := &App{toast: common.NewToastModel()}
		if cmd := app.handleWorkspaceSetupComplete(messages.WorkspaceSetupComplete{
			Workspace: ws, Err: process.ErrNoScriptConfigured,
		}); cmd != nil {
			t.Fatal("create-time no-setup completion must not toast")
		}
	})
}

func TestHandleScriptsDialogResult_CancelDiscardsEdits(t *testing.T) {
	ws := &data.Workspace{
		Name:    "feature",
		Repo:    "/repo/primary",
		Root:    "/repo/primary/ws",
		Scripts: data.ScriptsConfig{Run: "npm run dev"},
	}
	h, store, id := newScriptsTestHarness(t, ws)

	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})
	h.app.overlays.scripts.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	if cmd := h.app.handleScriptsDialogResult(common.ScriptsDialogResult{Canceled: true}); cmd != nil {
		t.Fatalf("expected nil cmd on cancel, got %v", cmd)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after cancel error = %v", err)
	}
	if reloaded.Scripts.Setup != "" || reloaded.Scripts.Run != "npm run dev" {
		t.Fatalf("cancel must not persist edits: %#v", reloaded.Scripts)
	}
	if h.app.overlays.scripts != nil || h.app.overlays.scriptsWorkspace != nil {
		t.Fatal("expected scriptsDialog/scriptsDialogWorkspace cleared after cancel")
	}
}
