package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

func setupTerminalOwnerModel(t *testing.T) *TerminalModel {
	t.Helper()
	m := NewTerminalModel()
	ws := &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
	wsID := string(ws.ID())
	m.workspace = ws
	m.focused = true

	ts := &TerminalState{
		VTerm:      vterm.New(10, 3),
		Running:    true,
		lastWidth:  10,
		lastHeight: 3,
	}
	tab := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 1",
		State: ts,
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
	return m
}

func TestTerminalLayerWithCursorOwner_HidesCursorWhenNotOwner(t *testing.T) {
	m := setupTerminalOwnerModel(t)

	layer := m.TerminalLayerWithCursorOwner(false)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected cursor hidden when sidebar pane does not own cursor")
	}
}

func TestTerminalLayerWithCursorOwner_ShowsCursorWhenOwner(t *testing.T) {
	m := setupTerminalOwnerModel(t)

	layer := m.TerminalLayerWithCursorOwner(true)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected cursor visible when sidebar pane owns cursor")
	}
}
