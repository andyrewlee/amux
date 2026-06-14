package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/vterm"
)

// ---------------------------------------------------------------------------
// updateOpenDiff
// ---------------------------------------------------------------------------

func TestUpdateOpenDiff_NilChangeIsNoOp(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")

	got, cmd := m.updateOpenDiff(messages.OpenDiff{Change: nil, Workspace: ws})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected nil command for a nil diff change")
	}
	if got := len(m.tabs.ByWorkspace[string(ws.ID())]); got != 0 {
		t.Fatalf("expected no diff tab created for nil change, got %d", got)
	}
}

func TestUpdateOpenDiff_NilWorkspaceReturnsError(t *testing.T) {
	m := newTestModel()

	got, cmd := m.updateOpenDiff(messages.OpenDiff{
		Change:    &git.Change{Path: "main.go", Kind: git.ChangeModified},
		Mode:      git.DiffModeUnstaged,
		Workspace: nil,
	})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd == nil {
		t.Fatal("expected an error command for a nil workspace")
	}
	errMsg, ok := cmd().(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", cmd())
	}
	if errMsg.Context != "creating diff viewer" {
		t.Fatalf("unexpected error context: %q", errMsg.Context)
	}
}

func TestUpdateOpenDiff_CreatesDiffTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.SetWorkspace(ws)

	// A brand-new diff path builds a diff.Model viewer in-process (no git CLI is
	// invoked until the returned command runs, which we intentionally do not do).
	got, cmd := m.updateOpenDiff(messages.OpenDiff{
		Change:    &git.Change{Path: "pkg/foo.go", Kind: git.ChangeModified},
		Mode:      git.DiffModeUnstaged,
		Workspace: ws,
	})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd == nil {
		t.Fatal("expected a command for a freshly created diff tab")
	}

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected exactly one diff tab created, got %d", len(tabs))
	}
	tab := tabs[0]
	if tab.Assistant != "diff" {
		t.Fatalf("expected diff assistant, got %q", tab.Assistant)
	}
	if tab.DiffViewer == nil {
		t.Fatal("expected the created tab to carry a diff viewer")
	}
	if m.tabs.ActiveByWorkspace[wsID] != 0 {
		t.Fatalf("expected the new diff tab to become active, got %d", m.tabs.ActiveByWorkspace[wsID])
	}
}

// ---------------------------------------------------------------------------
// updateWorkspaceDeleted
// ---------------------------------------------------------------------------

func TestUpdateWorkspaceDeleted_NilWorkspaceIsNoOp(t *testing.T) {
	m := newTestModel()
	other := newTestWorkspace("keep", "/repo/keep")
	otherID := string(other.ID())
	m.tabs.ByWorkspace[otherID] = []*Tab{{ID: TabID("keep"), Workspace: other, Running: true}}

	got, cmd := m.updateWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: nil})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected nil command for a nil workspace")
	}
	if got := len(m.tabs.ByWorkspace[otherID]); got != 1 {
		t.Fatalf("expected unrelated workspace tabs untouched, got %d", got)
	}
}

func TestUpdateWorkspaceDeleted_CleansUpTabs(t *testing.T) {
	m := newTestModel()
	target := newTestWorkspace("gone", "/repo/gone")
	keep := newTestWorkspace("keep", "/repo/keep")
	targetID := string(target.ID())
	keepID := string(keep.ID())

	doomed := &Tab{
		ID:        TabID("doomed"),
		Workspace: target,
		Running:   true,
		Terminal:  vterm.New(80, 24),
	}
	survivor := &Tab{ID: TabID("survivor"), Workspace: keep, Running: true}
	m.tabs.ByWorkspace[targetID] = []*Tab{doomed}
	m.tabs.ByWorkspace[keepID] = []*Tab{survivor}

	got, cmd := m.updateWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: target})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected nil command from workspace deletion")
	}

	if _, ok := m.tabs.ByWorkspace[targetID]; ok {
		t.Fatal("expected deleted workspace to be removed from the tab set")
	}
	if got := len(m.tabs.ByWorkspace[keepID]); got != 1 {
		t.Fatalf("expected the other workspace's tabs untouched, got %d", got)
	}
	// Cleanup tears down the tab: it is marked closed and its terminal released.
	if !doomed.isClosed() {
		t.Fatal("expected the deleted workspace's tab to be marked closed")
	}
	if doomed.Running {
		t.Fatal("expected the deleted workspace's tab to stop running")
	}
	doomed.mu.Lock()
	term := doomed.Terminal
	doomed.mu.Unlock()
	if term != nil {
		t.Fatal("expected the deleted tab's terminal to be released")
	}
}

// ---------------------------------------------------------------------------
// updateTabSelectionResult
// ---------------------------------------------------------------------------

func TestUpdateTabSelectionResult_EmptyClipboardIsNoOp(t *testing.T) {
	m := newTestModel()

	// An empty clipboard is a documented no-op in CopyToClipboardWithLog, so this
	// path never shells out to pbcopy and is safe to assert in a unit test.
	got, cmd := m.updateTabSelectionResult(tabSelectionResult{
		workspaceID: "ws",
		tabID:       TabID("tab"),
		clipboard:   "",
	})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected nil command from a selection result")
	}
}

// ---------------------------------------------------------------------------
// updateSelectionTickRequest
// ---------------------------------------------------------------------------

func TestUpdateSelectionTickRequest_SchedulesScrollTick(t *testing.T) {
	m := newTestModel()

	got, cmd := m.updateSelectionTickRequest(selectionTickRequest{
		workspaceID: "ws-7",
		tabID:       TabID("tab-7"),
		gen:         42,
	})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd == nil {
		t.Fatal("expected a tick command to be scheduled")
	}

	// Running the tick command yields the scroll tick carrying the same identity
	// and generation so a stale tab/generation can be filtered downstream.
	msg := cmd()
	tick, ok := msg.(selectionScrollTick)
	if !ok {
		t.Fatalf("expected selectionScrollTick, got %T", msg)
	}
	if tick.WorkspaceID != "ws-7" {
		t.Fatalf("expected workspace id carried through, got %q", tick.WorkspaceID)
	}
	if tick.TabID != TabID("tab-7") {
		t.Fatalf("expected tab id carried through, got %q", tick.TabID)
	}
	if tick.Gen != 42 {
		t.Fatalf("expected generation carried through, got %d", tick.Gen)
	}
}

// ---------------------------------------------------------------------------
// updateTabDiffCmd
// ---------------------------------------------------------------------------

func TestUpdateTabDiffCmd_ForwardsInnerCommand(t *testing.T) {
	m := newTestModel()
	sentinel := messages.Toast{Message: "diff-forwarded", Level: messages.ToastInfo}

	got, cmd := m.updateTabDiffCmd(tabDiffCmd{cmd: func() tea.Msg { return sentinel }})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd == nil {
		t.Fatal("expected the wrapped command to be forwarded")
	}
	out, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected the forwarded command to produce the inner message, got %T", cmd())
	}
	if out.Message != "diff-forwarded" {
		t.Fatalf("expected forwarded message payload, got %q", out.Message)
	}
}

func TestUpdateTabDiffCmd_NilInnerCommandIsForwardedAsNil(t *testing.T) {
	m := newTestModel()

	got, cmd := m.updateTabDiffCmd(tabDiffCmd{cmd: nil})
	if got != m {
		t.Fatal("expected same model pointer")
	}
	if cmd != nil {
		t.Fatal("expected a nil inner command to forward as nil")
	}
}
