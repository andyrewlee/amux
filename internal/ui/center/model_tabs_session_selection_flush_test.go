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
		ID:                 TabID("tab-1"),
		Assistant:          "claude",
		Workspace:          ws,
		Running:            true,
		pendingOutput:      []byte("buffered"),
		pendingOutputBytes: len("buffered"),
		ptyBytesReceived:   uint64(len("buffered")),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.tabSelectionChangedCmd(true)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd")
	}
	if !tab.catchUpPendingOutput {
		t.Fatalf("expected tab selection change to latch catch-up state")
	}
	if got, want := tab.catchUpTargetBytes, uint64(len(tab.pendingOutput)); got != want {
		t.Fatalf("catch-up target bytes = %d, want %d", got, want)
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
			if v.WorkspaceID != wsID || v.TabID != tab.ID || !v.CatchUp {
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

func TestTabSelectionChangedCmd_FlushesActorQueuedActiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	tab := &Tab{
		ID:                 TabID("tab-actor-queued"),
		Assistant:          "claude",
		Workspace:          ws,
		Running:            true,
		actorWritesPending: 1,
		actorQueuedBytes:   12,
		ptyBytesReceived:   12,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	cmd := m.tabSelectionChangedCmd(true)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd")
	}
	if !tab.catchUpPendingOutput {
		t.Fatalf("expected actor-queued backlog to latch catch-up state on selection")
	}
	if got, want := tab.catchUpTargetBytes, uint64(12); got != want {
		t.Fatalf("catch-up target bytes = %d, want %d", got, want)
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", msg)
	}

	var gotFlush bool
	for _, subcmd := range batch {
		if subcmd == nil {
			continue
		}
		if flush, ok := subcmd().(PTYFlush); ok {
			gotFlush = true
			if flush.WorkspaceID != wsID || flush.TabID != tab.ID || !flush.CatchUp {
				t.Fatalf("unexpected PTYFlush payload: %+v", flush)
			}
		}
	}
	if !gotFlush {
		t.Fatalf("expected PTYFlush command for actor-queued active tab")
	}
}
