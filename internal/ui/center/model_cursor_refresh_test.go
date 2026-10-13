package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYCursorRefresh_SchedulesWhileCursorTimersPending(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-cursor-refresh"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: time.Now(),
			lastPromptInputAt: time.Now(),
		},
		State: ptyio.State{
			CachedSnap: &compositor.VTermSnapshot{},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected cursor refresh tick to be scheduled while cursor timers are pending")
	}
	if tab.CachedSnap != nil {
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
		ID:        TabID("tab-cursor-refresh-dedupe"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
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
		ID:        TabID("tab-cursor-refresh-request"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
		State: ptyio.State{
			CachedSnap: &compositor.VTermSnapshot{},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
	if tab.CachedSnap != nil {
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

// TestScheduleChatCursorRefresh_StaleDeadlineRearms covers the panic-wedge
// case: a tick that was armed but never delivered (e.g. a handler panic
// between arm and delivery) leaves cursorRefreshPending latched with a
// deadline already in the past. That dead tick must not suppress re-arming —
// the next schedule call fences it via the gen bump and arms a fresh tick.
func TestScheduleChatCursorRefresh_StaleDeadlineRearms(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:        TabID("tab-cursor-stale"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// The dropped-delivery shape: pending latched, deadline already past.
	tab.cursorRefreshPending = true
	tab.cursorRefreshAt = now.Add(-time.Second)
	tab.cursorRefreshGen = 3

	cmd := m.scheduleChatCursorRefresh(tab, wsID, now)
	if cmd == nil {
		t.Fatal("a dead pending tick must not suppress re-arming — expected a fresh tick")
	}
	if tab.cursorRefreshGen != 4 {
		t.Fatalf("gen = %d, want 4 (stale tick fenced)", tab.cursorRefreshGen)
	}
	if !tab.cursorRefreshPending || !tab.cursorRefreshAt.After(now) {
		t.Fatalf("expected a live pending deadline after re-arm, got pending=%v at=%v",
			tab.cursorRefreshPending, tab.cursorRefreshAt)
	}
}

// TestScheduleChatCursorRefresh_LiveDeadlineStillSuppresses is the companion
// bound: a pending tick whose deadline is still in the future must keep
// suppressing redundant re-arms (no tick storms).
func TestScheduleChatCursorRefresh_LiveDeadlineStillSuppresses(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:        TabID("tab-cursor-live"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// Pending tick due sooner than what a re-schedule would pick — the
	// suppression shape (a later deadline would be pulled forward instead).
	tab.cursorRefreshPending = true
	tab.cursorRefreshAt = now.Add(time.Millisecond)
	tab.cursorRefreshGen = 5

	if cmd := m.scheduleChatCursorRefresh(tab, wsID, now); cmd != nil {
		t.Fatal("a live pending tick must still suppress a redundant re-arm")
	}
	if tab.cursorRefreshGen != 5 {
		t.Fatalf("gen = %d, want 5 (live tick undisturbed)", tab.cursorRefreshGen)
	}
	if !tab.cursorRefreshAt.Equal(now.Add(time.Millisecond)) {
		t.Fatalf("live deadline moved to %v", tab.cursorRefreshAt)
	}
}

// TestUpdatePTYCursorRefresh_DeliveryClearsAndRearms pins the healthy chain
// cadence: delivering the armed gen clears the latch, then the tail
// re-schedule arms the next tick while the tab is still active.
func TestUpdatePTYCursorRefresh_DeliveryClearsAndRearms(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:        TabID("tab-cursor-delivery"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	if arm := m.scheduleChatCursorRefresh(tab, wsID, now); arm == nil {
		t.Fatal("expected initial cursor refresh tick")
	}
	gen := tab.cursorRefreshGen

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID, Gen: gen})
	if cmd == nil {
		t.Fatal("expected the delivered tick to re-arm the next refresh while the tab is active")
	}
	if tab.cursorRefreshGen != gen+1 {
		t.Fatalf("gen = %d, want %d after delivery re-arm", tab.cursorRefreshGen, gen+1)
	}
	if !tab.cursorRefreshPending || !tab.cursorRefreshAt.After(now) {
		t.Fatalf("expected a freshly armed pending tick, got pending=%v at=%v",
			tab.cursorRefreshPending, tab.cursorRefreshAt)
	}
}

// TestUpdatePTYCursorRefresh_StaleGenSelfDrops pins the fencing half: a tick
// stamped with a superseded gen must leave the live pending tick untouched.
func TestUpdatePTYCursorRefresh_StaleGenSelfDrops(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()
	tab := &Tab{
		ID:        TabID("tab-cursor-stalegen"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		tabActivityState: tabActivityState{
			lastVisibleOutput: now,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	if arm := m.scheduleChatCursorRefresh(tab, wsID, now); arm == nil {
		t.Fatal("expected initial cursor refresh tick")
	}
	gen := tab.cursorRefreshGen
	due := tab.cursorRefreshAt

	if cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID, Gen: gen + 99}); cmd != nil {
		t.Fatal("a tick stamped with a foreign gen must self-drop")
	}
	if tab.cursorRefreshGen != gen || !tab.cursorRefreshPending || !tab.cursorRefreshAt.Equal(due) {
		t.Fatalf("stale gen delivery disturbed the live tick: gen=%d pending=%v at=%v",
			tab.cursorRefreshGen, tab.cursorRefreshPending, tab.cursorRefreshAt)
	}
}

func TestUpdatePTYCursorRefresh_InvalidatesCachedSnapshotForNonChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-cursor-refresh-non-chat"),
		Assistant: "bash",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		State: ptyio.State{
			CachedSnap: &compositor.VTermSnapshot{},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	cmd := m.updatePTYCursorRefresh(PTYCursorRefresh{WorkspaceID: wsID, TabID: tab.ID})
	if cmd != nil {
		t.Fatal("expected non-chat cursor refresh not to schedule chat timers")
	}
	if tab.CachedSnap != nil {
		t.Fatal("expected non-chat cursor refresh to invalidate cached snapshot")
	}
}
