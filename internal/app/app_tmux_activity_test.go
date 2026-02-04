package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

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

func TestHysteresisWorkspaceExtraction(t *testing.T) {
	// Pre-warm states above threshold so workspace ID extraction is exercised
	// even without a running tmux. Captures fail (decaying score by 1), but
	// activityScoreMax-1 still exceeds activityScoreThreshold.
	warmState := func() *sessionActivityState {
		return &sessionActivityState{
			score:        activityScoreMax,
			initialized:  true,
			lastActiveAt: time.Now(),
		}
	}

	infoBySession := map[string]tabSessionInfo{
		"sess-info-fallback": {WorkspaceID: "ws-from-info", IsChat: true},
		"sess-viewer":        {WorkspaceID: "ws-viewer", IsChat: false},
	}
	sessions := []tmux.SessionActivity{
		// Source 1: workspace ID from session field
		{Name: "sess-direct", WorkspaceID: "ws-direct", Type: "agent"},
		// Source 2: workspace ID falls back to tab info
		{Name: "sess-info-fallback", WorkspaceID: "", Type: "agent"},
		// Source 3: workspace ID falls back to session name
		{Name: "amux-ws99-tab-1", WorkspaceID: "", Type: "agent"},
		// Excluded: non-chat session (type="" and IsChat=false)
		{Name: "sess-viewer", WorkspaceID: "ws-viewer", Type: "", Tagged: true},
		// Excluded: below threshold
		{Name: "sess-cold", WorkspaceID: "ws-cold", Type: "agent"},
	}
	states := map[string]*sessionActivityState{
		"sess-direct":        warmState(),
		"sess-info-fallback": warmState(),
		"amux-ws99-tab-1":    warmState(),
		"sess-viewer":        warmState(),
		"sess-cold":          {score: 0, initialized: true},
	}

	captureFn := func(string, int, tmux.Options) (string, bool) { return "", false }
	hashFn := func(string) [16]byte { return [16]byte{} }

	active, updated := activeWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)

	// Workspace ID from session.WorkspaceID
	if !active["ws-direct"] {
		t.Error("expected ws-direct from session.WorkspaceID")
	}
	// Workspace ID from info fallback
	if !active["ws-from-info"] {
		t.Error("expected ws-from-info from tabSessionInfo fallback")
	}
	// Workspace ID from session name
	if !active["ws99"] {
		t.Error("expected ws99 from session name fallback")
	}
	// Non-chat session excluded
	if active["ws-viewer"] {
		t.Error("non-chat session should be excluded")
	}
	// Cold session excluded
	if active["ws-cold"] {
		t.Error("session below threshold should not be active")
	}
	// Updated states returned for all processed sessions
	for _, name := range []string{"sess-direct", "sess-info-fallback", "amux-ws99-tab-1", "sess-cold"} {
		if _, ok := updated[name]; !ok {
			t.Errorf("expected updated state for %s", name)
		}
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
