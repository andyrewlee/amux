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

// newEnvTestHarness builds a harness App wired to a real, temp-dir-backed
// WorkspaceStore (newAppShell attaches no services, so the harness has none
// by default -- see harness.go), then seeds and saves ws into that store so
// its ID resolves. It returns the harness, the concrete store (so tests can
// Load back what was persisted without an unchecked type assertion on the
// WorkspaceStore interface), and the saved workspace's ID.
func newEnvTestHarness(t *testing.T, ws *data.Workspace) (*Harness, *data.WorkspaceStore, data.WorkspaceID) {
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

func TestHandleShowWorkspaceEnvDialog_SeedsDialogExcludingReservedKeys(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env: map[string]string{
			"API_KEY":             "secret",
			"AMUX_WORKSPACE_ROOT": "poison",
		},
	}
	h, _, _ := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})

	if h.app.overlays.env == nil || !h.app.overlays.env.Visible() {
		t.Fatal("expected envDialog to be shown")
	}
	if h.app.overlays.envWorkspace != ws {
		t.Fatalf("envDialogWorkspace = %#v, want %#v", h.app.overlays.envWorkspace, ws)
	}
	got := h.app.overlays.env.Env()
	if got["API_KEY"] != "secret" {
		t.Fatalf("expected API_KEY row, got %#v", got)
	}
	if _, ok := got["AMUX_WORKSPACE_ROOT"]; ok {
		t.Fatalf("reserved key AMUX_WORKSPACE_ROOT must not be shown/editable, got %#v", got)
	}
}

func TestHandleShowWorkspaceEnvDialog_NilWorkspaceIsNoop(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: nil})
	if h.app.overlays.env != nil {
		t.Fatal("expected no dialog for a nil workspace")
	}
}

func TestHandleEnvDialogResult_PersistsEditedEnvAndUpdatesActiveWorkspace(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"NODE_ENV": "dev"},
	}
	h, store, id := newEnvTestHarness(t, ws)
	h.app.activeWorkspace = ws

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	// Edit NODE_ENV's value (cursor starts on row 0, the only row).
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected a success-toast cmd")
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after confirm error = %v", err)
	}
	if reloaded.Env["NODE_ENV"] != "devX" {
		t.Fatalf("persisted NODE_ENV = %q, want %q", reloaded.Env["NODE_ENV"], "devX")
	}

	if h.app.activeWorkspace.Env["NODE_ENV"] != "devX" {
		t.Fatalf("active workspace Env not updated in place: %#v", h.app.activeWorkspace.Env)
	}
	if h.app.overlays.env != nil || h.app.overlays.envWorkspace != nil {
		t.Fatal("expected envDialog/envDialogWorkspace cleared after confirm")
	}
	if !strings.Contains(h.app.toast.View(), "feature") {
		t.Fatalf("expected a success toast naming the workspace, got %q", h.app.toast.View())
	}
}

func TestHandleEnvDialogResult_RemovedPairIsDeletedFromEnv(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"KEEP": "1", "DROP": "2"},
	}
	h, store, id := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	// Cursor starts on row 0, which is "DROP" (sorted before "KEEP").
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})

	h.app.handleEnvDialogResult(common.EnvDialogResult{})

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := reloaded.Env["DROP"]; ok {
		t.Fatalf("expected DROP removed, got %#v", reloaded.Env)
	}
	if reloaded.Env["KEEP"] != "1" {
		t.Fatalf("expected KEEP untouched, got %#v", reloaded.Env)
	}
}

func TestHandleEnvDialogResult_CanceledDiscardsEditsWithoutPersisting(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"NODE_ENV": "dev"},
	}
	h, store, id := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{Canceled: true})
	if cmd != nil {
		t.Fatalf("expected no cmd on cancel, got one that emits %T", cmd())
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Env["NODE_ENV"] != "dev" {
		t.Fatalf("cancel must not persist: Env = %#v, want unchanged", reloaded.Env)
	}
	if h.app.overlays.env != nil || h.app.overlays.envWorkspace != nil {
		t.Fatal("expected envDialog/envDialogWorkspace cleared after cancel")
	}
}

func TestHandleEnvDialogResult_ReservedKeyNeverPersisted(t *testing.T) {
	// Simulate a hand-edited workspace.json that smuggled a reserved key in
	// (the dialog itself cannot produce one -- see the ui/common-level
	// tests -- but the apply path re-checks defensively).
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env: map[string]string{
			"CUSTOM_VAR":      "value",
			"AMUX_PORT":       "9999",
			"AMUX_PORT_RANGE": "9999-10000",
		},
	}
	h, store, id := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	h.app.handleEnvDialogResult(common.EnvDialogResult{})

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := reloaded.Env["AMUX_PORT"]; ok {
		t.Fatalf("reserved key AMUX_PORT must never be persisted, got %#v", reloaded.Env)
	}
	if _, ok := reloaded.Env["AMUX_PORT_RANGE"]; ok {
		t.Fatalf("reserved key AMUX_PORT_RANGE must never be persisted, got %#v", reloaded.Env)
	}
	if reloaded.Env["CUSTOM_VAR"] != "value" {
		t.Fatalf("expected CUSTOM_VAR preserved, got %#v", reloaded.Env)
	}
}

func TestHandleEnvDialogResult_NoDialogIsNoop(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	if cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{}); cmd != nil {
		t.Fatalf("expected nil cmd with no dialog open, got one that emits %T", cmd())
	}
}

// TestHandleEnvDialogResult_PersistsAddedPair drives the dialog's ctrl+a add
// mode end to end: a workspace with no Env map gets its first variable
// entirely through the UI, and it lands in the persisted store.
func TestHandleEnvDialogResult_PersistsAddedPair(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
	}
	h, store, id := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})

	d := h.app.overlays.env
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	for _, r := range "NEW_FLAG" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, r := range "on" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	cmd := h.app.handleEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected a success-toast cmd")
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after confirm error = %v", err)
	}
	if reloaded.Env["NEW_FLAG"] != "on" {
		t.Fatalf("persisted Env = %#v, want NEW_FLAG=on", reloaded.Env)
	}
}

// TestHandleShowWorkspaceEnvDialog_RejectsReservedKeyAdd pins the validator
// wiring: attempting to add a reserved name inside the dialog is rejected
// visibly (the add stays open with an error) rather than silently dropped
// at persist time.
func TestHandleShowWorkspaceEnvDialog_RejectsReservedKeyAdd(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
	}
	h, _, _ := newEnvTestHarness(t, ws)

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	d := h.app.overlays.env

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	for _, r := range "AMUX_WORKSPACE_ROOT" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := d.Env(); len(got) != 0 {
		t.Fatalf("reserved name must not be added: Env = %#v", got)
	}
	if view := d.View(); !strings.Contains(view, "reserved") {
		t.Fatalf("expected a visible 'reserved' rejection in the dialog, got:\n%s", view)
	}
}
