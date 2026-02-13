package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestTabSelectionChangedCmd_FlushesBufferedActiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:            TabID("tab-1"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		pendingOutput: []byte("buffered"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.tabSelectionChangedCmd(true)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", msg)
	}

	var gotSelection bool
	var gotFlush bool
	for _, subcmd := range batch {
		if subcmd == nil {
			continue
		}
		submsg := subcmd()
		switch v := submsg.(type) {
		case messages.TabSelectionChanged:
			gotSelection = true
			if v.WorkspaceID != wsID || v.ActiveIndex != 0 {
				t.Fatalf("unexpected TabSelectionChanged payload: %+v", v)
			}
		case PTYFlush:
			gotFlush = true
			if v.WorkspaceID != wsID || v.TabID != tab.ID {
				t.Fatalf("unexpected PTYFlush payload: %+v", v)
			}
		}
	}
	if !gotSelection {
		t.Fatalf("expected TabSelectionChanged command")
	}
	if !gotFlush {
		t.Fatalf("expected PTYFlush command for buffered active tab")
	}
}
