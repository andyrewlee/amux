package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// dispatchTabCreated runs a retained createDiffTab command and returns the
// TabCreated notification inside its batch, if any.
func dispatchTabCreated(t *testing.T, cmd tea.Cmd) (messages.TabCreated, bool) {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("creation command produced %T, want tea.BatchMsg", msg)
	}
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if created, ok := sub().(messages.TabCreated); ok {
			return created, true
		}
	}
	return messages.TabCreated{}, false
}

// TestCreateDiffTabDispatchRetainsCreatedIndexAfterSelection pins the
// plan-037 regression: the creation notification runs after Update returns,
// so it must carry the index captured at creation — not whatever selection
// happens to be active by then.
func TestCreateDiffTabDispatchRetainsCreatedIndexAfterSelection(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", t.TempDir())
	wsID := string(ws.ID())
	m.SetWorkspace(ws)

	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: TabID("tab-chat"), Name: "claude", Assistant: "claude", Workspace: ws, Running: true},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	cmd := m.createDiffTab(&git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, ws)
	if cmd == nil {
		t.Fatal("expected a creation command for a new diff tab")
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected the diff tab to append, got %d tabs", got)
	}

	// The user selects the chat tab before the notification executes.
	m.setActiveTabIdxForWorkspace(wsID, 0)

	created, ok := dispatchTabCreated(t, cmd)
	if !ok {
		t.Fatal("the creation command produced no TabCreated message")
	}
	if created.Index != 1 {
		t.Fatalf("TabCreated.Index = %d, want the created diff tab at 1", created.Index)
	}
	if created.Name != "Diff: main.go" {
		t.Fatalf("TabCreated.Name = %q, want %q", created.Name, "Diff: main.go")
	}
}

// TestCreateDiffTabDispatchRetainsCreatedIndexAfterMapRemoval is the harsher
// variant: the workspace's active-index entry is gone entirely by the time
// the notification runs. The captured index must still be reported.
func TestCreateDiffTabDispatchRetainsCreatedIndexAfterMapRemoval(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", t.TempDir())
	wsID := string(ws.ID())
	m.SetWorkspace(ws)

	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: TabID("tab-chat"), Name: "claude", Assistant: "claude", Workspace: ws, Running: true},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	cmd := m.createDiffTab(&git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, ws)
	if cmd == nil {
		t.Fatal("expected a creation command for a new diff tab")
	}

	delete(m.tabs.ActiveByWorkspace, wsID)

	created, ok := dispatchTabCreated(t, cmd)
	if !ok {
		t.Fatal("the creation command produced no TabCreated message")
	}
	if created.Index != 1 {
		t.Fatalf("TabCreated.Index = %d, want the created diff tab at 1", created.Index)
	}
}
