package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
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

func TestAddDetachedTab_DedupesDisplayName(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	m.tabsByWorkspace[wsID] = []*Tab{{
		ID:        TabID("tab-existing"),
		Name:      "codex",
		Assistant: "codex",
		Workspace: ws,
	}}

	m.addDetachedTab(ws, data.TabInfo{
		Assistant: "codex",
		Name:      "codex",
		Status:    "detached",
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	if tabs[1].Name != "codex 1" {
		t.Fatalf("detached tab name = %q, want %q", tabs[1].Name, "codex 1")
	}
}

func TestAddPlaceholderTab_DedupesDisplayName(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	m.tabsByWorkspace[wsID] = []*Tab{{
		ID:        TabID("tab-existing"),
		Name:      "codex",
		Assistant: "codex",
		Workspace: ws,
	}}

	_, _ = m.addPlaceholderTab(ws, data.TabInfo{
		Assistant: "codex",
		Name:      "codex",
		Status:    "running",
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	if tabs[1].Name != "codex 1" {
		t.Fatalf("placeholder tab name = %q, want %q", tabs[1].Name, "codex 1")
	}
}

func TestHandlePtyTabCreated_DedupesDisplayName(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	m.tabsByWorkspace[wsID] = []*Tab{{
		ID:        TabID("tab-existing"),
		Name:      "codex",
		Assistant: "codex",
		Workspace: ws,
	}}

	cmd := m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:   ws,
		Assistant:   "codex",
		DisplayName: "codex",
		Agent:       &appPty.Agent{Session: "sess-new"},
		Rows:        24,
		Cols:        80,
		Activate:    true,
	})
	if cmd == nil {
		t.Fatal("expected tab create command")
	}

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	if tabs[1].Name != "codex 1" {
		t.Fatalf("created tab name = %q, want %q", tabs[1].Name, "codex 1")
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
