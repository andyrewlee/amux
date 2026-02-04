package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
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
		lastOutputAt: now.Add(-200 * time.Millisecond), // Within 500ms window
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
		lastOutputAt: now.Add(-200 * time.Millisecond), // Within 500ms window but not a chat tab
	}
	if m.IsTabActive(nonChat) {
		t.Fatalf("expected non-chat tab to be inactive even with output")
	}

	detached := &Tab{
		Assistant:    "claude",
		Workspace:    ws,
		Running:      true,
		Detached:     true,
		lastOutputAt: now.Add(-200 * time.Millisecond), // Within 500ms window but detached
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
		lastOutputAt: now.Add(-200 * time.Millisecond), // Within 500ms window
	}
	viewer := &Tab{
		Assistant:    "viewer",
		Workspace:    ws2,
		Running:      true,
		lastOutputAt: now.Add(-200 * time.Millisecond), // Within 500ms window but not a chat tab
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

func TestIsTabActiveSuppressedDuringInput(t *testing.T) {
	m := newTestModel()
	now := time.Now()

	ws := newTestWorkspace("ws", "/repo/ws")

	// Tab has pending output (like terminal echo from typing) but user recently typed
	withPendingButRecentInput := &Tab{
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastInputAt:   now.Add(-100 * time.Millisecond), // User typed 100ms ago
		pendingOutput: []byte("user input echo"),
	}
	if m.IsTabActive(withPendingButRecentInput) {
		t.Fatalf("expected tab to be inactive when user recently typed, even with pending output")
	}

	// Tab has pending output and user typed a while ago - should be active
	withPendingAndOldInput := &Tab{
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastInputAt:   now.Add(-2 * time.Second), // User typed 2s ago
		pendingOutput: []byte("agent output"),
	}
	if !m.IsTabActive(withPendingAndOldInput) {
		t.Fatalf("expected tab to be active when pending output and no recent input")
	}
}
