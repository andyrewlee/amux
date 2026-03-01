package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYOutputSetsAndClearsCursorRefreshSchedule(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(10, 3)

	tab := &Tab{
		ID:        TabID("tab-cursor-refresh"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
		Running:   true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.SetWorkspace(ws)

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("hello"),
	})

	tab.mu.Lock()
	scheduled := tab.cursorRefreshScheduled
	tab.mu.Unlock()
	if !scheduled {
		t.Fatal("expected cursor refresh scheduling after chat PTY output")
	}

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})
	if cmd == nil {
		t.Fatal("expected cursor refresh to reschedule while suppression deadline is still in the future")
	}

	tab.mu.Lock()
	stillScheduled := tab.cursorRefreshScheduled
	tab.cursorRefreshDueAt = time.Now().Add(-time.Millisecond)
	tab.mu.Unlock()
	if !stillScheduled {
		t.Fatal("expected cursor refresh scheduling to remain active until due time elapses")
	}

	_ = m.updatePTYCursorRefresh(PTYCursorRefresh{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	tab.mu.Lock()
	cleared := tab.cursorRefreshScheduled
	tab.mu.Unlock()
	if cleared {
		t.Fatal("expected cursor refresh scheduling flag to clear on refresh tick")
	}
}
