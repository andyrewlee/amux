package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYCursorRefresh_SchedulesWhileCursorTimersPending(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-cursor-refresh"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastVisibleOutput: time.Now(),
		lastPromptInputAt: time.Now(),
		cachedSnap:        &compositor.VTermSnapshot{},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected cursor refresh tick to be scheduled while cursor timers are pending")
	}
	if tab.cachedSnap != nil {
		t.Fatal("expected cursor refresh to invalidate cached snapshot")
	}
	if tab.cursorRefreshGen == 0 {
		t.Fatal("expected cursor refresh generation to advance")
	}
}

func TestScheduleChatCursorRefresh_DeduplicatesLaterRefreshDeadline(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:                TabID("tab-cursor-refresh-dedupe"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastVisibleOutput: now,
	}

	first := m.scheduleChatCursorRefresh(tab, wsID, now)
	if first == nil {
		t.Fatal("expected initial cursor refresh tick")
	}
	firstGen := tab.cursorRefreshGen
	firstDue := tab.cursorRefreshAt

	tab.mu.Lock()
	tab.lastVisibleOutput = now.Add(100 * time.Millisecond)
	tab.mu.Unlock()

	second := m.scheduleChatCursorRefresh(tab, wsID, now.Add(100*time.Millisecond))
	if second != nil {
		t.Fatal("expected later refresh deadline to reuse existing pending tick")
	}
	if tab.cursorRefreshGen != firstGen {
		t.Fatalf("expected refresh generation to stay at %d, got %d", firstGen, tab.cursorRefreshGen)
	}
	if !tab.cursorRefreshAt.Equal(firstDue) {
		t.Fatalf("expected refresh deadline to stay at %v, got %v", firstDue, tab.cursorRefreshAt)
	}
}

func TestUpdatePTYCursorRefresh_RequestPreservesPendingTimer(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:                TabID("tab-cursor-refresh-request"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastVisibleOutput: now,
		cachedSnap:        &compositor.VTermSnapshot{},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	first := m.scheduleChatCursorRefresh(tab, wsID, now)
	if first == nil {
		t.Fatal("expected initial cursor refresh tick")
	}
	firstGen := tab.cursorRefreshGen
	firstDue := tab.cursorRefreshAt

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID})
	if cmd != nil {
		t.Fatal("expected refresh request to reuse the existing pending timer")
	}
	if tab.cachedSnap != nil {
		t.Fatal("expected refresh request to invalidate cached snapshot")
	}
	if tab.cursorRefreshGen != firstGen {
		t.Fatalf("expected refresh generation to stay at %d, got %d", firstGen, tab.cursorRefreshGen)
	}
	if !tab.cursorRefreshAt.Equal(firstDue) {
		t.Fatalf("expected refresh deadline to stay at %v, got %v", firstDue, tab.cursorRefreshAt)
	}
	if !tab.cursorRefreshPending {
		t.Fatal("expected refresh request to keep the pending timer armed")
	}
}

func TestUpdatePTYCursorRefresh_InvalidatesCachedSnapshotForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:         TabID("tab-cursor-refresh-non-chat"),
		Assistant:  "bash",
		Workspace:  ws,
		Terminal:   vterm.New(80, 24),
		cachedSnap: &compositor.VTermSnapshot{},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID})
	if cmd != nil {
		t.Fatal("expected non-chat cursor refresh not to schedule chat timers")
	}
	if tab.cachedSnap != nil {
		t.Fatal("expected non-chat cursor refresh to invalidate cached snapshot")
	}
}
