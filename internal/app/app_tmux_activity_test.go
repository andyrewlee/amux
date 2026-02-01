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
		{Name: "sess-running", WorkspaceID: "ws1", Type: "agent", Tagged: true},
		{Name: "sess-detached", WorkspaceID: "ws2", Type: "agent", Tagged: true},
		{Name: "sess-stopped", WorkspaceID: "ws3", Type: "agent", Tagged: true},
		{Name: "sess-empty", WorkspaceID: "", Type: "agent", Tagged: true},
		{Name: "sess-missing", WorkspaceID: "ws6", Type: "agent", Tagged: true},
		{Name: "sess-viewer", WorkspaceID: "ws5", Type: "", Tagged: true},
		{Name: "amux-ws7-tab-1", WorkspaceID: "", Type: "", Tagged: false},
		{Name: "amux-ws8-term-tab-1", WorkspaceID: "", Type: "", Tagged: true},
		{Name: "other-app-tab-99", WorkspaceID: "", Type: ""},
	}
	active := activeWorkspaceIDsFromSessionActivity(infoBySession, sessions)
	if len(active) != 5 {
		t.Fatalf("expected 5 active workspaces, got %d", len(active))
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
	session2 := tmux.SessionActivity{Name: "amux-ws1-tab-1", Type: "", Tagged: true}
	if !isChatSession(session2, tabSessionInfo{}, false) {
		t.Fatal("tagged amux session with -tab- should be classified as chat")
	}

	// Sessions with explicit type should use type regardless of name
	session3 := tmux.SessionActivity{Name: "random-name", Type: "agent"}
	if !isChatSession(session3, tabSessionInfo{}, false) {
		t.Fatal("session with type=agent should be classified as chat")
	}
}

func TestSessionActivityHysteresis(t *testing.T) {
	state := &sessionActivityState{}

	// Test 1: First hash initializes but doesn't set active
	hash1 := [16]byte{1, 2, 3}
	state.lastHash = hash1
	state.initialized = true
	state.score = 0
	if state.score >= activityScoreThreshold {
		t.Fatal("newly initialized session should not be active")
	}

	// Test 2: Single change bumps score but should NOT reach threshold (threshold=3)
	hash2 := [16]byte{4, 5, 6}
	state.score += 2 // first change: score=2
	state.lastHash = hash2
	if state.score >= activityScoreThreshold {
		t.Fatalf("single change (score=%d) should NOT reach threshold %d", state.score, activityScoreThreshold)
	}

	// Test 3: Second consecutive change should reach threshold
	state.score += 2 // second change: score=4
	if state.score < activityScoreThreshold {
		t.Fatalf("two consecutive changes (score=%d) should reach threshold %d", state.score, activityScoreThreshold)
	}

	// Test 4: No change decays score
	state.score-- // score=3, still at threshold
	if state.score < activityScoreThreshold {
		t.Fatal("score should still be at threshold after one decay")
	}
	state.score-- // score=2, below threshold
	if state.score >= activityScoreThreshold {
		t.Fatal("decayed score should be below threshold")
	}

	// Test 5: Multiple consecutive changes accumulate to max
	state.score = 0
	state.score += 2 // first change
	state.score += 2 // second change
	state.score += 2 // third change
	if state.score > activityScoreMax {
		state.score = activityScoreMax
	}
	if state.score != activityScoreMax {
		t.Fatalf("consecutive changes should accumulate to max (%d), got %d", activityScoreMax, state.score)
	}

	// Test 6: Decay from max
	for i := 0; i < 7; i++ {
		state.score--
		if state.score < 0 {
			state.score = 0
		}
	}
	if state.score != 0 {
		t.Fatalf("should decay to 0 after enough ticks without changes, got %d", state.score)
	}
}
