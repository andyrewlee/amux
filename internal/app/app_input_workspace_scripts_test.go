package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
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
