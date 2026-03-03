package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestWorkspaceIDRefreshesWhenWorkspacePointerChanges(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	m.setWorkspace(ws)
	if got := m.workspaceID(); got == "" {
		t.Fatal("expected non-empty workspace ID")
	}

	// Simulate a direct workspace pointer swap bypassing setWorkspace while keeping
	// repo/root unchanged; workspaceID() should still invalidate and refresh.
	m.workspace = data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	m.workspaceIDCached = "stale-id"

	if got := m.workspaceID(); got != string(m.workspace.ID()) {
		t.Fatalf("expected workspace ID %q after pointer-swap refresh, got %q", m.workspace.ID(), got)
	}
}
