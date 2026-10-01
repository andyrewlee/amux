package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Status-dialog cleanup section: the read-only tombstone probe rides the
// open cmd, and the section renders only when a marker exists — three
// honest states, never a fabricated stage, never a mutation.

// cleanupStatusApp wires the status surface to a real workspace store so
// the tombstone probe has something to read.
func cleanupStatusApp(t *testing.T, meta string) (*App, *data.WorkspaceStore) {
	t.Helper()
	store := data.NewWorkspaceStore(meta)
	scripts := process.NewScriptRunner(6200, 10)
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, store, scripts, ""),
		width:            120,
		height:           40,
	}
	return app, store
}

func TestWorkspaceStatusCleanup_NoTombstoneNoSection(t *testing.T) {
	app, store := cleanupStatusApp(t, t.TempDir())
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	statusInterval(t, app, ws)
	d := app.overlays.runOutput
	if d == nil || !d.Visible() {
		t.Fatal("status dialog did not open")
	}
	if view := ansi.Strip(d.View()); strings.Contains(view, "cleanup") {
		t.Fatalf("cleanup section rendered without a tombstone:\n%s", view)
	}
}

func TestWorkspaceStatusCleanup_Interrupted(t *testing.T) {
	app, store := cleanupStatusApp(t, t.TempDir())
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id, _ := ws.StoredID()
	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	statusInterval(t, app, ws)
	view := ansi.Strip(app.overlays.runOutput.View())
	for _, want := range []string{
		"cleanup",
		"interrupted — worktree still present; workspace remains usable",
		"workspace delete",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("interrupted view missing %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceStatusCleanup_Pending(t *testing.T) {
	app, store := cleanupStatusApp(t, t.TempDir())
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id, _ := ws.StoredID()
	if err := store.MarkDeleting(id); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	ws.Root = t.TempDir() + "/removed" // worktree already gone
	statusInterval(t, app, ws)
	view := ansi.Strip(app.overlays.runOutput.View())
	for _, want := range []string{
		"cleanup",
		"pending — worktree already removed; retry automatic on next load",
		"startup recovery",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("pending view missing %q:\n%s", want, view)
		}
	}
}

// The unknown state renders when the probe reports it — covered at the
// message boundary since the bool probe cannot currently produce it
// (WorkspaceIdentitySet always yields an ID for a non-nil workspace).
func TestWorkspaceStatusCleanup_Unknown(t *testing.T) {
	app, _ := cleanupStatusApp(t, t.TempDir())
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	app.handleWorkspaceStatusReady(workspaceStatusReadyMsg{
		token:   app.overlays.runOutputToken,
		ws:      ws,
		cleanup: workspacesvc.WorkspaceCleanupUnknown,
	})
	view := ansi.Strip(app.overlays.runOutput.View())
	if !strings.Contains(view, "unknown — could not read recovery state") {
		t.Fatalf("unknown state missing:\n%s", view)
	}
}

// The whole open path — cmd probe + ready apply — must issue zero
// mutating calls on the store.
func TestWorkspaceStatusCleanup_ProbeNeverMutates(t *testing.T) {
	var mark, clr, del, save, update, rename int
	fake := &testutil.FakeWorkspaceStore{
		IsDeletingFunc:               func(data.WorkspaceID) bool { return true },
		MarkDeletingFunc:             func(data.WorkspaceID) error { mark++; return nil },
		ClearDeletingFunc:            func(data.WorkspaceID) error { clr++; return nil },
		DeleteFunc:                   func(data.WorkspaceID) error { del++; return nil },
		SaveFunc:                     func(*data.Workspace) error { save++; return nil },
		UpdateFunc:                   func(data.WorkspaceID, func(*data.Workspace) (bool, error)) error { update++; return nil },
		RenameFunc:                   func(data.WorkspaceID, string) error { rename++; return nil },
		ResolvedDefaultAssistantFunc: func() string { return "" },
	}
	scripts := process.NewScriptRunner(6200, 10)
	app := &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, fake, scripts, ""),
		width:            120,
		height:           40,
	}
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	cmd := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	if cmd == nil {
		t.Fatal("status open returned no fetch cmd")
	}
	ready, ok := cmd().(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("fetch emitted %T", cmd())
	}
	if ready.cleanup != workspacesvc.WorkspaceCleanupInterrupted {
		t.Fatalf("probe state = %v, want Interrupted", ready.cleanup)
	}
	app.handleWorkspaceStatusReady(ready)
	if mark+clr+del+save+update+rename != 0 {
		t.Fatalf("status open mutated the store: mark=%d clr=%d del=%d save=%d update=%d rename=%d",
			mark, clr, del, save, update, rename)
	}
}
