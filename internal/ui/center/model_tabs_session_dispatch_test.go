package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// detachTabAt, DetachTabByID and DetachActiveTab are thin dispatchers in
// model_tabs_session.go that resolve a tab from an index / id / active
// selection and then delegate to detachTab. The tests below exercise the
// resolution and boundary logic of those dispatchers directly; detachTab's
// own state transitions are covered in model_tabs_session_detach_test.go.

// setCurrentWorkspace wires a workspace as the model's current one so that
// getTabs / getActiveTabIdx (which key off m.workspaceID()) resolve to it.
func setCurrentWorkspace(m *Model, ws *data.Workspace) string {
	m.workspace = ws
	return string(ws.ID())
}

func TestDetachTabAt_OutOfRangeReturnsNil(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	chatTab := func() *Tab {
		return &Tab{ID: TabID("tab-1"), Assistant: "claude", Workspace: ws, Running: true}
	}

	tests := []struct {
		name  string
		tabs  []*Tab
		index int
	}{
		{name: "empty tabs", tabs: nil, index: 0},
		{name: "negative index", tabs: []*Tab{chatTab()}, index: -1},
		{name: "index equals length", tabs: []*Tab{chatTab()}, index: 1},
		{name: "index beyond length", tabs: []*Tab{chatTab()}, index: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			wsID := setCurrentWorkspace(m, ws)
			m.tabs.ByWorkspace[wsID] = tc.tabs

			if cmd := m.detachTabAt(tc.index); cmd != nil {
				t.Fatalf("expected nil cmd for %s, got %T", tc.name, cmd())
			}
		})
	}
}

func TestDetachTabAt_ValidIndexDetachesSelectedTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := setCurrentWorkspace(m, ws)
	keep := &Tab{ID: TabID("keep"), Assistant: "claude", Workspace: ws, Running: true}
	target := &Tab{ID: TabID("target"), Assistant: "claude", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{keep, target}

	cmd := m.detachTabAt(1)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for in-range index")
	}
	msg, ok := cmd().(messages.TabDetached)
	if !ok {
		t.Fatalf("expected messages.TabDetached, got %T", cmd())
	}
	if msg.Index != 1 {
		t.Fatalf("detached index = %d, want 1", msg.Index)
	}
	if msg.WorkspaceID != wsID {
		t.Fatalf("workspaceID = %q, want %q", msg.WorkspaceID, wsID)
	}

	target.mu.Lock()
	targetDetached := target.Detached
	target.mu.Unlock()
	if !targetDetached {
		t.Fatal("expected the selected tab to be detached")
	}

	keep.mu.Lock()
	keepDetached := keep.Detached
	keep.mu.Unlock()
	if keepDetached {
		t.Fatal("expected the non-selected tab to be untouched")
	}
}

func TestDetachTabAt_NonChatTabReturnsToast(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := setCurrentWorkspace(m, ws)
	// "vim" is not in the configured assistant roster, so it is not a chat tab.
	nonChat := &Tab{ID: TabID("term"), Assistant: "vim", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{nonChat}

	cmd := m.detachTabAt(0)
	if cmd == nil {
		t.Fatal("expected toast cmd for non-chat tab")
	}
	toast, ok := cmd().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmd())
	}
	if toast.Message != "Only assistant tabs can be detached" {
		t.Fatalf("toast message = %q", toast.Message)
	}

	nonChat.mu.Lock()
	detached := nonChat.Detached
	nonChat.mu.Unlock()
	if detached {
		t.Fatal("expected non-chat tab not to be detached")
	}
}

func TestDetachTabByID_EmptyWorkspaceReturnsNil(t *testing.T) {
	m := newTestModel()
	if cmd := m.DetachTabByID("", TabID("anything")); cmd != nil {
		t.Fatalf("expected nil cmd for empty workspace id, got %T", cmd())
	}
}

func TestDetachTabByID_UnknownIDReturnsNil(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: TabID("a"), Assistant: "claude", Workspace: ws, Running: true},
		{ID: TabID("b"), Assistant: "claude", Workspace: ws, Running: true},
	}

	if cmd := m.DetachTabByID(wsID, TabID("missing")); cmd != nil {
		t.Fatalf("expected nil cmd for unknown tab id, got %T", cmd())
	}
}

func TestDetachTabByID_SkipsNilAndClosedTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	closed := &Tab{ID: TabID("dup"), Assistant: "claude", Workspace: ws, Running: true}
	closed.markClosing()
	live := &Tab{ID: TabID("dup"), Assistant: "claude", Workspace: ws, Running: true}
	// Both a nil entry and a closed tab share the target id; only the live tab
	// at a later position should be detached.
	m.tabs.ByWorkspace[wsID] = []*Tab{nil, closed, live}

	cmd := m.DetachTabByID(wsID, TabID("dup"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd when a live matching tab exists")
	}
	if _, ok := cmd().(messages.TabDetached); !ok {
		t.Fatalf("expected messages.TabDetached, got %T", cmd())
	}

	live.mu.Lock()
	liveDetached := live.Detached
	live.mu.Unlock()
	if !liveDetached {
		t.Fatal("expected the live matching tab to be detached")
	}

	closed.mu.Lock()
	closedDetached := closed.Detached
	closed.mu.Unlock()
	if closedDetached {
		t.Fatal("expected the closed tab to be skipped, not detached")
	}
}

func TestDetachTabByID_DetachesByIndexNotPosition(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	first := &Tab{ID: TabID("first"), Assistant: "claude", Workspace: ws, Running: true}
	second := &Tab{ID: TabID("second"), Assistant: "claude", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{first, second}

	cmd := m.DetachTabByID(wsID, TabID("second"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg, ok := cmd().(messages.TabDetached)
	if !ok {
		t.Fatalf("expected messages.TabDetached, got %T", cmd())
	}
	if msg.Index != 1 {
		t.Fatalf("detached index = %d, want 1 (slice position of the match)", msg.Index)
	}
	if msg.WorkspaceID != wsID {
		t.Fatalf("workspaceID = %q, want %q", msg.WorkspaceID, wsID)
	}
}

func TestDetachActiveTab_NoTabsReturnsNil(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := setCurrentWorkspace(m, ws)
	m.tabs.ByWorkspace[wsID] = nil

	if cmd := m.DetachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd with no tabs, got %T", cmd())
	}
}

func TestDetachActiveTab_ActiveIndexOutOfRangeReturnsNil(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := setCurrentWorkspace(m, ws)
	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: TabID("only"), Assistant: "claude", Workspace: ws, Running: true},
	}
	// Active index points past the end of the slice.
	m.tabs.ActiveByWorkspace[wsID] = 3

	if cmd := m.DetachActiveTab(); cmd != nil {
		t.Fatalf("expected nil cmd for stale active index, got %T", cmd())
	}
}

func TestDetachActiveTab_DetachesActiveSelection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := setCurrentWorkspace(m, ws)
	first := &Tab{ID: TabID("first"), Assistant: "claude", Workspace: ws, Running: true}
	active := &Tab{ID: TabID("active"), Assistant: "claude", Workspace: ws, Running: true}
	m.tabs.ByWorkspace[wsID] = []*Tab{first, active}
	m.tabs.ActiveByWorkspace[wsID] = 1

	cmd := m.DetachActiveTab()
	if cmd == nil {
		t.Fatal("expected non-nil cmd for valid active tab")
	}
	msg, ok := cmd().(messages.TabDetached)
	if !ok {
		t.Fatalf("expected messages.TabDetached, got %T", cmd())
	}
	if msg.Index != 1 {
		t.Fatalf("detached active index = %d, want 1", msg.Index)
	}

	active.mu.Lock()
	activeDetached := active.Detached
	active.mu.Unlock()
	if !activeDetached {
		t.Fatal("expected the active tab to be detached")
	}

	first.mu.Lock()
	firstDetached := first.Detached
	first.mu.Unlock()
	if firstDetached {
		t.Fatal("expected the inactive tab to be untouched")
	}
}
