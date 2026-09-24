package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func panickingCmd() tea.Cmd {
	return func() tea.Msg {
		panic("op exploded")
	}
}

// TestWrapLifecycleCmdPanicYieldsTypedFailure pins the release contract: a
// panicked lifecycle op must surface as the op's typed failure (carrying the
// workspace identity), never a bare messages.Error — the generic type would
// toast without releasing the mutating mark or advancing a bulk drain.
func TestWrapLifecycleCmdPanicYieldsTypedFailure(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Branch: "ws", Repo: "/repo", Root: "/repo/.amux/workspaces/ws"}
	proj := &data.Project{Name: "repo", Path: "/repo"}

	cases := []struct {
		kind    lifecycleOpKind
		wantMsg func(tea.Msg) bool
	}{
		{lifecycleOpDelete, func(m tea.Msg) bool {
			f, ok := m.(messages.WorkspaceDeleteFailed)
			return ok && f.Workspace == ws && f.Project == proj && len(f.WorkspaceIDs) > 0 && f.Err != nil
		}},
		{lifecycleOpShelve, func(m tea.Msg) bool {
			f, ok := m.(messages.WorkspaceShelveFailed)
			return ok && f.Workspace == ws && len(f.WorkspaceIDs) > 0 && f.Err != nil
		}},
		{lifecycleOpRestore, func(m tea.Msg) bool {
			f, ok := m.(messages.WorkspaceRestoreFailed)
			return ok && f.Workspace == ws && len(f.WorkspaceIDs) > 0 && f.Err != nil
		}},
		{lifecycleOpCreate, func(m tea.Msg) bool {
			f, ok := m.(messages.WorkspaceCreateFailed)
			return ok && f.Workspace == ws && f.Err != nil
		}},
	}
	for _, tc := range cases {
		got := wrapLifecycleCmd(panickingCmd(), tc.kind, proj, ws)()
		if !tc.wantMsg(got) {
			t.Fatalf("kind %s: panic produced %T, want the op's typed failure carrying the workspace", tc.kind, got)
		}
	}
}

// TestWrapLifecycleCmdPanicReleasesBulkDrain covers the wedge end-to-end at
// the handler level: the typed failure a panic produces must clear the
// mutating mark AND advance the bulk queue — the two things a generic
// messages.Error silently never did.
func TestWrapLifecycleCmdPanicReleasesBulkDrain(t *testing.T) {
	app := newBulkTestApp()
	targets := []bulkTarget{makeBulkTarget("a"), makeBulkTarget("b")}

	app.startBulkOp(bulkOpPurge, targets)
	if app.bulk.headID != targets[0].markID {
		t.Fatalf("head = %q, want %q", app.bulk.headID, targets[0].markID)
	}

	msg := wrapLifecycleCmd(panickingCmd(), lifecycleOpDelete, targets[0].project, targets[0].workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("panic produced %T, want WorkspaceDeleteFailed", msg)
	}

	cmd := app.handleWorkspaceDeleteFailed(failed)
	if cmd == nil {
		t.Fatal("expected the failure handler to advance the drain")
	}
	if app.isWorkspaceMutationInFlight(string(targets[0].workspace.ID())) {
		t.Fatal("mutating mark leaked after panic-failed op")
	}
	if app.bulk.headID != targets[1].markID {
		t.Fatalf("bulk head did not advance: %q, want %q", app.bulk.headID, targets[1].markID)
	}
	if app.bulk.failed != 1 {
		t.Fatalf("bulk.failed = %d, want 1", app.bulk.failed)
	}
}

// TestWrapLifecycleCmdPanicSingleOpReleasesMark covers the non-bulk arm: a
// panicked single delete releases the mark through the same handler.
func TestWrapLifecycleCmdPanicSingleOpReleasesMark(t *testing.T) {
	app := newBulkTestApp()
	target := makeBulkTarget("solo")

	app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: target.project, Workspace: target.workspace})
	if !app.isWorkspaceMutationInFlight(string(target.workspace.ID())) {
		t.Fatal("expected mutating mark after delete dispatch")
	}

	msg := wrapLifecycleCmd(panickingCmd(), lifecycleOpDelete, target.project, target.workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("panic produced %T, want WorkspaceDeleteFailed", msg)
	}
	_ = app.handleWorkspaceDeleteFailed(failed)
	if app.isWorkspaceMutationInFlight(string(target.workspace.ID())) {
		t.Fatal("mutating mark leaked after panic-failed single op")
	}
}

// TestWrapLifecycleCmdNilWorkspace guards the unmarked arm: with no workspace
// there is no lifecycle mark to release, so the panic reports through the
// generic error path rather than a typed failure with a nil workspace.
func TestWrapLifecycleCmdNilWorkspace(t *testing.T) {
	got := wrapLifecycleCmd(panickingCmd(), lifecycleOpDelete, nil, nil)()
	if _, ok := got.(messages.Error); !ok {
		t.Fatalf("nil-workspace panic produced %T, want messages.Error", got)
	}
}

// TestWrapLifecycleCmdPassThrough confirms non-panicking ops are untouched.
func TestWrapLifecycleCmdPassThrough(t *testing.T) {
	want := messages.WorkspaceDeleted{Workspace: &data.Workspace{Name: "ws"}}
	got := wrapLifecycleCmd(func() tea.Msg { return want }, lifecycleOpDelete, nil, nil)()
	deleted, ok := got.(messages.WorkspaceDeleted)
	if !ok || deleted.Workspace.Name != "ws" {
		t.Fatalf("pass-through altered the message: %#v", got)
	}
}
