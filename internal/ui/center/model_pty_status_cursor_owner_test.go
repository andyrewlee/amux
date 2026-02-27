package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerWithCursorOwner_HidesCursorWhenNotOwner(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-owner"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayerWithCursorOwner(false)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected cursor hidden when center pane does not own cursor")
	}
}

func TestTerminalLayerWithCursorOwner_ShowsCursorWhenOwner(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-owner"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayerWithCursorOwner(true)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected cursor visible when center pane owns cursor")
	}
}
