package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestActiveWorkspaceIDsFromSessionActivity(t *testing.T) {
	infoBySession := map[string]tabSessionInfo{
		"sess-running":  {Status: "running", WorkspaceID: "ws1", IsChat: true},
		"sess-detached": {Status: "detached", WorkspaceID: "ws2", IsChat: true},
		"sess-stopped":  {Status: "stopped", WorkspaceID: "ws3", IsChat: true},
		"sess-empty":    {Status: "", WorkspaceID: "ws4", IsChat: true},
		"sess-viewer":   {Status: "running", WorkspaceID: "ws5", IsChat: false},
	}
	sessions := []tmux.SessionActivity{
		{Name: "sess-running", WorkspaceID: "ws1", Type: "agent"},
		{Name: "sess-detached", WorkspaceID: "ws2", Type: "agent"},
		{Name: "sess-stopped", WorkspaceID: "ws3", Type: "agent"},
		{Name: "sess-empty", WorkspaceID: "", Type: "agent"},
		{Name: "sess-missing", WorkspaceID: "ws6", Type: "agent"},
		{Name: "sess-viewer", WorkspaceID: "ws5", Type: ""},
		{Name: "amux-ws7-tab-1", WorkspaceID: "", Type: ""},
		{Name: "amux-ws8-term-tab-1", WorkspaceID: "", Type: ""},
		{Name: "other-app-tab-99", WorkspaceID: "", Type: ""},
	}
	active := activeWorkspaceIDsFromSessionActivity(infoBySession, sessions)
	if len(active) != 6 {
		t.Fatalf("expected 6 active workspaces, got %d", len(active))
	}
	if !active["ws1"] {
		t.Fatalf("expected ws1 to be active")
	}
	if !active["ws4"] {
		t.Fatalf("expected ws4 to be active for empty status")
	}
	if !active["ws2"] || !active["ws3"] {
		t.Fatalf("expected ws2 and ws3 to be active despite stale status")
	}
	if !active["ws7"] {
		t.Fatalf("expected ws7 to be active for amux session without stored info")
	}
	if !active["ws6"] {
		t.Fatalf("expected ws6 to be active for tagged session without stored info")
	}
	if active["ws5"] {
		t.Fatalf("unexpected active workspaces: %v", active)
	}
	// Non-amux session with -tab- in name should NOT match the heuristic
	if active["other"] {
		t.Fatalf("non-amux session with -tab- in name should not match: %v", active)
	}
}

func TestIsChatSession_NonAmuxPrefix(t *testing.T) {
	// Sessions without "amux-" prefix should not match the name heuristic
	session := tmux.SessionActivity{Name: "other-app-tab-99", Type: ""}
	if isChatSession(session, tabSessionInfo{}, false) {
		t.Fatal("session without amux- prefix should not be classified as chat")
	}

	// Sessions with "amux-" prefix and -tab- should match
	session2 := tmux.SessionActivity{Name: "amux-ws1-tab-1", Type: ""}
	if !isChatSession(session2, tabSessionInfo{}, false) {
		t.Fatal("amux session with -tab- should be classified as chat")
	}

	// Sessions with explicit type should use type regardless of name
	session3 := tmux.SessionActivity{Name: "random-name", Type: "agent"}
	if !isChatSession(session3, tabSessionInfo{}, false) {
		t.Fatal("session with type=agent should be classified as chat")
	}
}
