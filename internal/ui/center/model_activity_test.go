package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
)

func newTestModel() *Model {
	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"claude": {},
			"codex":  {},
		},
	}
	return New(cfg)
}

func newTestWorkspace(name, root string) *data.Workspace {
	return &data.Workspace{
		Name: name,
		Repo: root,
		Root: root,
	}
}

func TestIsTabActiveChatOnly(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	ws := newTestWorkspace("ws", "/repo/ws")
	activeChat := &Tab{
		Assistant:    "claude",
		Workspace:    ws,
		Running:      true,
		lastOutputAt: now.Add(-1 * time.Second),
	}
	m.tabsByWorkspace[string(ws.ID())] = []*Tab{activeChat}

	if !m.IsTabActive(activeChat) {
		t.Fatalf("expected chat tab to be active with recent output")
	}
}

func TestIsTabActiveIgnoresDetachedAndNonChat(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	ws := newTestWorkspace("ws", "/repo/ws")
	nonChat := &Tab{
		Assistant:    "vim",
		Workspace:    ws,
		Running:      true,
		lastOutputAt: now.Add(-1 * time.Second),
	}
	if m.IsTabActive(nonChat) {
		t.Fatalf("expected non-chat tab to be inactive even with output")
	}

	detached := &Tab{
		Assistant:    "claude",
		Workspace:    ws,
		Running:      true,
		Detached:     true,
		lastOutputAt: now.Add(-1 * time.Second),
	}
	if m.IsTabActive(detached) {
		t.Fatalf("expected detached chat tab to be inactive")
	}
}

func TestGetActiveWorkspaceIDsChatOnly(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	ws1 := newTestWorkspace("ws1", "/repo/ws1")
	ws2 := newTestWorkspace("ws2", "/repo/ws2")

	activeChat := &Tab{
		Assistant:    "claude",
		Workspace:    ws1,
		Running:      true,
		lastOutputAt: now.Add(-1 * time.Second),
	}
	viewer := &Tab{
		Assistant:    "viewer",
		Workspace:    ws2,
		Running:      true,
		lastOutputAt: now.Add(-1 * time.Second),
	}

	m.tabsByWorkspace[string(ws1.ID())] = []*Tab{activeChat}
	m.tabsByWorkspace[string(ws2.ID())] = []*Tab{viewer}

	ids := m.GetActiveWorkspaceIDs()
	if len(ids) != 1 || ids[0] != string(ws1.ID()) {
		t.Fatalf("expected only ws1 to be active, got %v", ids)
	}
}

func TestIsTabActiveIdle(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	ws := newTestWorkspace("ws", "/repo/ws")
	idle := &Tab{
		Assistant:    "claude",
		Workspace:    ws,
		Running:      true,
		lastOutputAt: now.Add(-3 * time.Second),
	}
	if m.IsTabActive(idle) {
		t.Fatalf("expected idle chat tab to be inactive")
	}
}
