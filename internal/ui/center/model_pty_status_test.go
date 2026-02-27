package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalLayerForcesVisibleCursorForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Write([]byte("\x1b[?25l"))

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.CursorHidden {
		t.Fatal("expected chat tab cursor to remain visible despite DECTCEM hide")
	}
}

func TestTerminalLayerPreservesCursorHiddenForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)
	term.Write([]byte("\x1b[?25l"))

	m.tabsByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-non-chat"),
			Assistant: "bash",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.CursorHidden {
		t.Fatal("expected non-chat tab to honor DECTCEM hide")
	}
}
