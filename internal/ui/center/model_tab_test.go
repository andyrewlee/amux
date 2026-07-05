package center

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/diff"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newTabWithID builds a minimal in-memory tab with the given id and assistant.
func newTabWithID(id TabID, assistant string, ws *data.Workspace) *Tab {
	return &Tab{
		ID:        id,
		Name:      assistant,
		Assistant: assistant,
		Workspace: ws,
		Running:   true,
	}
}

func TestMarkClosing(t *testing.T) {
	t.Run("sets closing flag without setting closed", func(t *testing.T) {
		tab := &Tab{}
		tab.markClosing()

		if atomic.LoadUint32(&tab.closing) != 1 {
			t.Fatalf("expected closing flag to be set after markClosing")
		}
		if atomic.LoadUint32(&tab.closed) != 0 {
			t.Fatalf("expected closed flag to remain unset after markClosing")
		}
	})

	t.Run("makes isClosed report true", func(t *testing.T) {
		tab := &Tab{}
		if tab.isClosed() {
			t.Fatalf("fresh tab should not report closed")
		}
		tab.markClosing()
		if !tab.isClosed() {
			t.Fatalf("tab should report closed once closing is latched")
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		tab := &Tab{}
		tab.markClosing()
		tab.markClosing()
		if atomic.LoadUint32(&tab.closing) != 1 {
			t.Fatalf("expected closing flag to stay set across repeated calls")
		}
	})

	t.Run("nil receiver is a no-op", func(t *testing.T) {
		var tab *Tab
		// Must not panic.
		tab.markClosing()
	})
}

func TestNormalizePTYAccountingIgnoresNegativeCounters(t *testing.T) {
	tab := &Tab{
		pendingOutputBytes: -10,
		ptyBytesSettled:    42,
		tabActorWriteState: tabActorWriteState{
			actorQueuedBytes: -5,
		},
	}

	tab.normalizePTYAccountingLocked()

	if tab.ptyBytesReceived != 42 {
		t.Fatalf("ptyBytesReceived = %d, want settled count 42", tab.ptyBytesReceived)
	}
}

func TestSetActiveTabIdx(t *testing.T) {
	t.Run("records index for the active workspace", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)
		m.tabs.ByWorkspace[wsID] = []*Tab{
			newTabWithID("tab-a", "claude", ws),
			newTabWithID("tab-b", "codex", ws),
		}

		m.setActiveTabIdx(1)

		if got := m.tabs.ActiveByWorkspace[wsID]; got != 1 {
			t.Fatalf("expected active index 1, got %d", got)
		}
		if got := m.getActiveTabIdx(); got != 1 {
			t.Fatalf("expected getActiveTabIdx to return 1, got %d", got)
		}
	})

	t.Run("out-of-range index is still stored but focus is not marked", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)
		only := newTabWithID("tab-a", "claude", ws)
		m.tabs.ByWorkspace[wsID] = []*Tab{only}

		m.setActiveTabIdx(5)

		if got := m.tabs.ActiveByWorkspace[wsID]; got != 5 {
			t.Fatalf("expected out-of-range index 5 to be stored, got %d", got)
		}
		if !only.lastFocusedAt.IsZero() {
			t.Fatalf("expected no focus timestamp for out-of-range index")
		}
	})

	t.Run("marks the selected tab as focused", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)
		focused := newTabWithID("tab-b", "codex", ws)
		m.tabs.ByWorkspace[wsID] = []*Tab{
			newTabWithID("tab-a", "claude", ws),
			focused,
		}

		m.setActiveTabIdx(1)

		if focused.lastFocusedAt.IsZero() {
			t.Fatalf("expected the newly active tab to record a focus timestamp")
		}
	})

	t.Run("updates post-write visibility to follow the active tab", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)
		first := newTabWithID("tab-a", "claude", ws)
		second := newTabWithID("tab-b", "codex", ws)
		m.tabs.ByWorkspace[wsID] = []*Tab{first, second}

		m.setActiveTabIdx(0)
		if !first.postWriteVisible() {
			t.Fatalf("expected first tab to be post-write visible when active")
		}
		if second.postWriteVisible() {
			t.Fatalf("expected second tab to be hidden when first is active")
		}

		m.setActiveTabIdx(1)
		if first.postWriteVisible() {
			t.Fatalf("expected first tab to become hidden after switching active")
		}
		if !second.postWriteVisible() {
			t.Fatalf("expected second tab to be post-write visible once active")
		}
	})

	t.Run("empty workspace id is ignored", func(t *testing.T) {
		m := newTestModel()
		// No workspace set: workspaceID() returns "".
		m.setActiveTabIdx(3)
		if _, ok := m.tabs.ActiveByWorkspace[""]; ok {
			t.Fatalf("expected empty workspace id to be skipped, not recorded")
		}
	})
}

func TestRemoveTab(t *testing.T) {
	tabIDs := func(tabs []*Tab) []TabID {
		ids := make([]TabID, 0, len(tabs))
		for _, tab := range tabs {
			ids = append(ids, tab.ID)
		}
		return ids
	}

	tests := []struct {
		name      string
		ids       []TabID
		removeIdx int
		wantIDs   []TabID
		wantBump  bool
	}{
		{
			name:      "remove first tab",
			ids:       []TabID{"a", "b", "c"},
			removeIdx: 0,
			wantIDs:   []TabID{"b", "c"},
			wantBump:  true,
		},
		{
			name:      "remove middle tab",
			ids:       []TabID{"a", "b", "c"},
			removeIdx: 1,
			wantIDs:   []TabID{"a", "c"},
			wantBump:  true,
		},
		{
			name:      "remove last tab",
			ids:       []TabID{"a", "b", "c"},
			removeIdx: 2,
			wantIDs:   []TabID{"a", "b"},
			wantBump:  true,
		},
		{
			name:      "remove only tab",
			ids:       []TabID{"a"},
			removeIdx: 0,
			wantIDs:   []TabID{},
			wantBump:  true,
		},
		{
			name:      "negative index is ignored",
			ids:       []TabID{"a", "b"},
			removeIdx: -1,
			wantIDs:   []TabID{"a", "b"},
			wantBump:  false,
		},
		{
			name:      "index past end is ignored",
			ids:       []TabID{"a", "b"},
			removeIdx: 2,
			wantIDs:   []TabID{"a", "b"},
			wantBump:  false,
		},
		{
			name:      "remove from empty slice is ignored",
			ids:       []TabID{},
			removeIdx: 0,
			wantIDs:   []TabID{},
			wantBump:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			ws := newTestWorkspace("ws", "/repo/ws")
			wsID := string(ws.ID())
			m.setWorkspace(ws)

			tabs := make([]*Tab, 0, len(tc.ids))
			for _, id := range tc.ids {
				tabs = append(tabs, newTabWithID(id, "claude", ws))
			}
			m.tabs.ByWorkspace[wsID] = tabs

			revBefore := m.tabsRevision
			m.removeTab(tc.removeIdx)
			revAfter := m.tabsRevision

			got := tabIDs(m.tabs.ByWorkspace[wsID])
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("expected %v remaining tabs, got %v", tc.wantIDs, got)
			}
			for i := range tc.wantIDs {
				if got[i] != tc.wantIDs[i] {
					t.Fatalf("expected remaining tabs %v, got %v", tc.wantIDs, got)
				}
			}

			bumped := revAfter != revBefore
			if bumped != tc.wantBump {
				t.Fatalf("expected revision bump=%v (before=%d after=%d)", tc.wantBump, revBefore, revAfter)
			}
		})
	}
}

func TestCleanupWorkspace(t *testing.T) {
	t.Run("nil workspace is a no-op", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.tabs.ByWorkspace[wsID] = []*Tab{newTabWithID("a", "claude", ws)}

		revBefore := m.tabsRevision
		m.CleanupWorkspace(nil)

		if _, ok := m.tabs.ByWorkspace[wsID]; !ok {
			t.Fatalf("expected existing workspace tabs to be untouched for nil input")
		}
		if m.tabsRevision != revBefore {
			t.Fatalf("expected no revision bump for nil workspace")
		}
	})

	t.Run("removes all tab tracking for the workspace", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)

		tab := newTabWithID("a", "claude", ws)
		tab.Terminal = vterm.New(80, 24)
		tab.State = ptyio.State{PendingOutput: []byte("buffered output")}
		m.tabs.ByWorkspace[wsID] = []*Tab{tab}
		m.tabs.ActiveByWorkspace[wsID] = 0

		revBefore := m.tabsRevision
		m.CleanupWorkspace(ws)

		if _, ok := m.tabs.ByWorkspace[wsID]; ok {
			t.Fatalf("expected workspace tab slice to be deleted")
		}
		if _, ok := m.tabs.ActiveByWorkspace[wsID]; ok {
			t.Fatalf("expected workspace active-index entry to be deleted")
		}
		if m.tabsRevision <= revBefore {
			t.Fatalf("expected revision bump after cleanup, before=%d after=%d", revBefore, m.tabsRevision)
		}
	})

	t.Run("closes each tab and tears down its resources", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)

		tab := newTabWithID("a", "claude", ws)
		tab.Terminal = vterm.New(80, 24)
		tab.State = ptyio.State{
			PendingOutput: []byte("buffered output"),
			CachedSnap:    &compositor.VTermSnapshot{},
		}
		tab.pendingOutputBytes = len(tab.PendingOutput)
		tab.DiffViewer = &diff.Model{}
		m.tabs.ByWorkspace[wsID] = []*Tab{tab}

		m.CleanupWorkspace(ws)

		if !tab.isClosed() {
			t.Fatalf("expected tab to be marked closed after cleanup")
		}
		if atomic.LoadUint32(&tab.closed) != 1 {
			t.Fatalf("expected closed flag to be latched")
		}
		if tab.Terminal != nil {
			t.Fatalf("expected terminal to be released")
		}
		if tab.DiffViewer != nil {
			t.Fatalf("expected diff viewer to be released")
		}
		if tab.Workspace != nil {
			t.Fatalf("expected workspace reference to be cleared")
		}
		if tab.Running {
			t.Fatalf("expected tab to be marked not running")
		}
		if tab.CachedSnap != nil {
			t.Fatalf("expected cached snapshot to be cleared")
		}
		if tab.PendingOutput != nil || tab.pendingOutputBytes != 0 {
			t.Fatalf("expected pending PTY output to be reset")
		}
	})

	t.Run("closes an open pty trace file", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		m.setWorkspace(ws)

		trace, err := os.Create(filepath.Join(t.TempDir(), "trace.log"))
		if err != nil {
			t.Fatalf("failed to create trace file: %v", err)
		}

		tab := newTabWithID("a", "claude", ws)
		tab.ptyTraceFile = trace
		m.tabs.ByWorkspace[wsID] = []*Tab{tab}

		m.CleanupWorkspace(ws)

		if tab.ptyTraceFile != nil {
			t.Fatalf("expected trace file handle to be cleared")
		}
		if !tab.ptyTraceClosed {
			t.Fatalf("expected trace file to be marked closed")
		}
		// A second close should fail because cleanup already closed it.
		if err := trace.Close(); err == nil {
			t.Fatalf("expected trace file to already be closed by cleanup")
		}
	})

	t.Run("only affects the named workspace", func(t *testing.T) {
		m := newTestModel()
		target := newTestWorkspace("target", "/repo/target")
		other := newTestWorkspace("other", "/repo/other")
		targetID := string(target.ID())
		otherID := string(other.ID())

		otherTab := newTabWithID("keep", "claude", other)
		m.tabs.ByWorkspace[targetID] = []*Tab{newTabWithID("drop", "claude", target)}
		m.tabs.ByWorkspace[otherID] = []*Tab{otherTab}
		m.tabs.ActiveByWorkspace[otherID] = 0
		m.setWorkspace(target)

		m.CleanupWorkspace(target)

		if _, ok := m.tabs.ByWorkspace[targetID]; ok {
			t.Fatalf("expected target workspace tabs to be removed")
		}
		kept, ok := m.tabs.ByWorkspace[otherID]
		if !ok || len(kept) != 1 || kept[0].ID != "keep" {
			t.Fatalf("expected unrelated workspace tabs to be preserved, got %v", kept)
		}
		if otherTab.isClosed() {
			t.Fatalf("expected unrelated tab to remain open")
		}
	})
}
