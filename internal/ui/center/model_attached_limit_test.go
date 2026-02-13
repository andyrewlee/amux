package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
)

func newLimitTab(ws *data.Workspace, id, assistant string, createdAt int64) *Tab {
	return &Tab{
		ID:        TabID(id),
		Workspace: ws,
		Assistant: assistant,
		Running:   true,
		createdAt: createdAt,
	}
}

func TestEnforceAttachedAgentTabLimitDetachesOldest(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := &data.Workspace{Name: "ws", Repo: "/tmp/ws", Root: "/tmp/ws"}
	wsID := string(ws.ID())

	m.tabsByWorkspace[wsID] = []*Tab{
		newLimitTab(ws, "tab-1", "claude", 1),
		newLimitTab(ws, "tab-2", "claude", 2),
		newLimitTab(ws, "tab-3", "claude", 3),
		newLimitTab(ws, "tab-4", "claude", 4),
	}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.EnforceAttachedAgentTabLimit(2)
	if cmd == nil {
		t.Fatal("expected enforcement command, got nil")
	}
	_ = cmd()

	if !m.tabsByWorkspace[wsID][0].Detached {
		t.Fatalf("expected oldest tab to detach")
	}
	if !m.tabsByWorkspace[wsID][1].Detached {
		t.Fatalf("expected second-oldest tab to detach")
	}
	if m.tabsByWorkspace[wsID][2].Detached {
		t.Fatalf("expected third tab to remain attached")
	}
	if m.tabsByWorkspace[wsID][3].Detached {
		t.Fatalf("expected newest tab to remain attached")
	}
}

func TestEnforceAttachedAgentTabLimitSkipsNonChatTabs(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := &data.Workspace{Name: "ws", Repo: "/tmp/ws2", Root: "/tmp/ws2"}
	wsID := string(ws.ID())

	m.tabsByWorkspace[wsID] = []*Tab{
		newLimitTab(ws, "tab-1", "claude", 1),
		newLimitTab(ws, "tab-2", "vim", 2),
		newLimitTab(ws, "tab-3", "codex", 3),
		newLimitTab(ws, "tab-4", "claude", 4),
	}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.EnforceAttachedAgentTabLimit(2)
	if cmd == nil {
		t.Fatal("expected enforcement command, got nil")
	}
	_ = cmd()

	if !m.tabsByWorkspace[wsID][0].Detached {
		t.Fatalf("expected oldest chat tab to detach")
	}
	if m.tabsByWorkspace[wsID][1].Detached {
		t.Fatalf("expected non-chat tab to remain untouched")
	}
	if m.tabsByWorkspace[wsID][2].Detached {
		t.Fatalf("expected latest chat tab to remain attached")
	}
	if m.tabsByWorkspace[wsID][3].Detached {
		t.Fatalf("expected newest chat tab to remain attached")
	}
}

func TestEnforceAttachedAgentTabLimitIsGlobalAcrossWorkspaces(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws1 := &data.Workspace{Name: "ws1", Repo: "/tmp/ws1", Root: "/tmp/ws1"}
	ws2 := &data.Workspace{Name: "ws2", Repo: "/tmp/ws2", Root: "/tmp/ws2"}
	ws1ID := string(ws1.ID())
	ws2ID := string(ws2.ID())

	m.tabsByWorkspace[ws1ID] = []*Tab{newLimitTab(ws1, "tab-a", "claude", 1)}
	m.tabsByWorkspace[ws2ID] = []*Tab{
		newLimitTab(ws2, "tab-b", "claude", 2),
		newLimitTab(ws2, "tab-c", "codex", 3),
	}
	m.activeTabByWorkspace[ws1ID] = 0
	m.activeTabByWorkspace[ws2ID] = 0

	cmd := m.EnforceAttachedAgentTabLimit(2)
	if cmd == nil {
		t.Fatal("expected enforcement command, got nil")
	}
	_ = cmd()

	if !m.tabsByWorkspace[ws1ID][0].Detached {
		t.Fatalf("expected oldest global tab to detach")
	}
	if m.tabsByWorkspace[ws2ID][0].Detached {
		t.Fatalf("expected second oldest global tab to remain attached")
	}
	if m.tabsByWorkspace[ws2ID][1].Detached {
		t.Fatalf("expected newest global tab to remain attached")
	}
}

func TestEnforceAttachedAgentTabLimitPreservesActiveTabWhenPossible(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	ws := &data.Workspace{Name: "ws", Repo: "/tmp/ws-active", Root: "/tmp/ws-active"}
	wsID := string(ws.ID())

	m.SetWorkspace(ws)
	m.tabsByWorkspace[wsID] = []*Tab{
		newLimitTab(ws, "tab-old-active", "claude", 1),
		newLimitTab(ws, "tab-new", "claude", 2),
	}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.EnforceAttachedAgentTabLimit(1)
	if cmd == nil {
		t.Fatal("expected enforcement command, got nil")
	}
	_ = cmd()

	if m.tabsByWorkspace[wsID][0].Detached {
		t.Fatalf("expected active tab to stay attached")
	}
	if !m.tabsByWorkspace[wsID][1].Detached {
		t.Fatalf("expected non-active tab to detach first")
	}
}
