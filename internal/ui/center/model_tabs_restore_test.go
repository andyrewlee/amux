package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestAddDetachedTab_SetsLastFocusedFromCreatedAt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	createdAt := time.Now().Add(-time.Hour).Unix()

	m.addDetachedTab(ws, data.TabInfo{
		Assistant:   "claude",
		Name:        "Claude",
		SessionName: "sess-detached",
		CreatedAt:   createdAt,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].lastFocusedAt != time.Unix(createdAt, 0) {
		t.Fatalf("expected lastFocusedAt=%s, got %s", time.Unix(createdAt, 0), tabs[0].lastFocusedAt)
	}
}

func TestAddPlaceholderTab_SetsLastFocusedFromCreatedAt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	createdAt := time.Now().Add(-2 * time.Hour).Unix()

	_, _ = m.addPlaceholderTab(ws, data.TabInfo{
		Assistant: "claude",
		Name:      "Claude",
		CreatedAt: createdAt,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].lastFocusedAt != time.Unix(createdAt, 0) {
		t.Fatalf("expected lastFocusedAt=%s, got %s", time.Unix(createdAt, 0), tabs[0].lastFocusedAt)
	}
}

func TestRestoreTabsFromWorkspace_MarksReattachInFlightForRunningTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	ws.OpenTabs = []data.TabInfo{
		{
			Assistant:   "claude",
			Name:        "Claude",
			Status:      "running",
			SessionName: "sess-running",
		},
	}
	wsID := string(ws.ID())

	if cmd := m.RestoreTabsFromWorkspace(ws); cmd == nil {
		t.Fatalf("expected restore command for running tab")
	}

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 restored tab, got %d", len(tabs))
	}
	tab := tabs[0]
	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	detached := tab.Detached
	tab.mu.Unlock()
	if !detached {
		t.Fatalf("expected restored placeholder tab to be detached before reattach result")
	}
	if !inFlight {
		t.Fatalf("expected restored placeholder tab to start with reattachInFlight=true")
	}
}

func TestAutoReattachActiveTabOnSelection_SkipsRestoreInFlightPlaceholder(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	ws.OpenTabs = []data.TabInfo{
		{
			Assistant:   "claude",
			Name:        "Claude",
			Status:      "running",
			SessionName: "sess-running",
		},
	}
	wsID := string(ws.ID())

	_ = m.RestoreTabsFromWorkspace(ws)
	m.workspace = ws
	m.activeTabByWorkspace[wsID] = 0

	if cmd := m.autoReattachActiveTabOnSelection(); cmd != nil {
		t.Fatalf("expected auto reattach to skip while restore reattach is in flight")
	}
}
