package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

func newDiffViewerTab(ws *data.Workspace, id TabID, focused bool) *Tab {
	dv := diff.New(ws, &git.Change{Path: "pkg/foo.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24)
	dv.SetFocused(focused)
	return &Tab{ID: id, Workspace: ws, Assistant: "diff", DiffViewer: dv}
}

// A wrapped diff result must reach the tab that issued the load, not the tab
// that happens to be active when it arrives. 'q' produces CloseTab only from
// a focused viewer, so a result addressed to the focused-but-inactive tab1
// yields a command while the same keys on active-but-unfocused tab2 would be
// ignored — the command's presence proves the route.
func TestDiffResultMsgRoutesToIssuingTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.SetWorkspace(ws)
	wsID := string(ws.ID())

	tab1 := newDiffViewerTab(ws, "tab-issuer", true)
	tab2 := newDiffViewerTab(ws, "tab-active", false)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab1, tab2}
	m.tabs.ActiveByWorkspace[wsID] = 1

	key := tea.KeyPressMsg{Code: 'q', Text: "q"}

	_, cmd := m.Update(diffResultMsg{WorkspaceID: wsID, TabID: tab1.ID, Inner: key})
	found := false
	for _, msg := range drainBatch(cmd) {
		if _, ok := msg.(messages.CloseTab); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("expected CloseTab from the issuing tab's focused viewer")
	}

	// Addressed to the unfocused tab2: the viewer ignores 'q' — no command.
	_, cmd = m.Update(diffResultMsg{WorkspaceID: wsID, TabID: tab2.ID, Inner: key})
	if cmd != nil {
		t.Fatalf("expected no command for an unfocused viewer, got %v", drainBatch(cmd))
	}
}

// A result whose issuing tab is gone must be dropped quietly — no panic, no
// command, and no delivery to whatever tab is active.
func TestDiffResultMsgClosedTabDropped(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.SetWorkspace(ws)
	wsID := string(ws.ID())

	tab2 := newDiffViewerTab(ws, "tab-active", true)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab2}
	m.tabs.ActiveByWorkspace[wsID] = 0

	key := tea.KeyPressMsg{Code: 'q', Text: "q"}
	_, cmd := m.Update(diffResultMsg{WorkspaceID: wsID, TabID: "tab-gone", Inner: key})
	if cmd != nil {
		t.Fatalf("expected no command for a result addressed to a closed tab, got %v", drainBatch(cmd))
	}
}

// createDiffTab must install the result wrapper so Init()'s async result
// arrives addressed to the new tab.
func TestCreateDiffTabInstallsResultWrapper(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	m.SetWorkspace(ws)
	wsID := string(ws.ID())

	cmd := m.createDiffTab(&git.Change{Path: "pkg/foo.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, ws)
	msgs := drainBatch(cmd)

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected one diff tab, got %d", len(tabs))
	}
	var wrapped *diffResultMsg
	for _, msg := range msgs {
		if w, ok := msg.(diffResultMsg); ok {
			wrapped = &w
		}
	}
	if wrapped == nil {
		t.Fatalf("expected a diffResultMsg among emitted messages, got %v", msgs)
	}
	if wrapped.TabID != tabs[0].ID || wrapped.WorkspaceID != wsID {
		t.Fatalf("result addressed to (%s,%s), want (%s,%s)", wrapped.WorkspaceID, wrapped.TabID, wsID, tabs[0].ID)
	}
}
