package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestHysteresisWorkspaceExtraction(t *testing.T) {
	// Pre-warm states above threshold so workspace ID extraction is exercised
	// even without a running tmux. Captures fail (decaying score by 1), but
	// ScoreMax-1 still exceeds ScoreThreshold.
	warmState := func() *SessionState {
		return &SessionState{
			Score:        ScoreMax,
			Initialized:  true,
			LastActiveAt: time.Now(),
		}
	}

	infoBySession := map[string]SessionInfo{
		"sess-info-fallback": {WorkspaceID: "ws-from-info", IsChat: true},
		"sess-viewer":        {WorkspaceID: "ws-viewer", IsChat: false},
		"sess-mismatch":      {WorkspaceID: "ws-canonical", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		// Source 1: workspace ID from session field
		{Name: "sess-direct", WorkspaceID: "ws-direct", Type: "agent"},
		// Source 2: workspace ID falls back to tab info
		{Name: "sess-info-fallback", WorkspaceID: "", Type: "agent"},
		// Source 3: workspace ID falls back to session name
		{Name: "amux-ws99-tab-1", WorkspaceID: "", Type: "agent"},
		// Source 4: known-session metadata wins over stale/mismatched tmux tag
		{Name: "sess-mismatch", WorkspaceID: "ws-stale-tag", Type: "agent"},
		// Excluded: non-chat session (type="" and IsChat=false)
		{Name: "sess-viewer", WorkspaceID: "ws-viewer", Type: "", Tagged: true},
		// Excluded: below threshold
		{Name: "sess-cold", WorkspaceID: "ws-cold", Type: "agent"},
	}
	states := map[string]*SessionState{
		"sess-direct":        warmState(),
		"sess-info-fallback": warmState(),
		"amux-ws99-tab-1":    warmState(),
		"sess-mismatch":      warmState(),
		"sess-viewer":        warmState(),
		"sess-cold":          {Score: 0, Initialized: true},
	}

	captureFn := func(string, int, tmux.Options) (string, bool) { return "", false }
	hashFn := func(string) [16]byte { return [16]byte{} }

	active, updated, _ := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)

	// Workspace ID from session.WorkspaceID
	if !active["ws-direct"] {
		t.Error("expected ws-direct from session.WorkspaceID")
	}
	// Workspace ID from info fallback
	if !active["ws-from-info"] {
		t.Error("expected ws-from-info from SessionInfo fallback")
	}
	// Workspace ID from session name
	if !active["ws99"] {
		t.Error("expected ws99 from session name fallback")
	}
	// Workspace ID from known tab metadata should override stale tag values
	if !active["ws-canonical"] {
		t.Error("expected ws-canonical from known tab metadata")
	}
	if active["ws-stale-tag"] {
		t.Error("stale tag workspace ID should not be used when known metadata is present")
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
	for _, name := range []string{"sess-direct", "sess-info-fallback", "amux-ws99-tab-1", "sess-mismatch", "sess-cold"} {
		if _, ok := updated[name]; !ok {
			t.Errorf("expected updated state for %s", name)
		}
	}
}

func TestHysteresisFirstObservationSeedsBaseline(t *testing.T) {
	// A session captured for the first time (empty states map — an app
	// start or an ownership-handoff reset) must seed the baseline hash and
	// score 0, NOT claim activity: a first capture carries no evidence the
	// pane is producing output now. Activity requires observed deltas on
	// later scans — the fast path for genuinely fresh sessions is the tag
	// classifier's fresh-tag routes, which do not pass through here.
	infoBySession := map[string]SessionInfo{
		"amux-abc-tab-1": {WorkspaceID: "ws-abc", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "amux-abc-tab-1", WorkspaceID: "ws-abc", Type: "agent"},
	}
	// Empty states map -- session has never been seen before
	states := map[string]*SessionState{}

	captureFn := func(string, int, tmux.Options) (string, bool) { return "some output", true }
	hashFn := func(content string) [16]byte { return [16]byte{1} }

	active, updated, _ := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)

	if active["ws-abc"] {
		t.Fatal("first observation must not claim activity without an observed delta")
	}
	st := updated["amux-abc-tab-1"]
	if st == nil {
		t.Fatal("expected updated state for session")
	}
	if st.Score != 0 {
		t.Fatalf("seeded score should be 0, got %d", st.Score)
	}
	if !st.Initialized {
		t.Fatal("state should be initialized after first capture")
	}

	// A session that keeps changing content crosses the threshold and
	// becomes active — the seeded baseline is what makes the delta
	// observable. +2 per changed scan: 0 -> 2 -> 4 (>= threshold).
	contentEpoch := 0
	changedHashFn := func(string) [16]byte { return [16]byte{byte(2 + contentEpoch)} }
	contentEpoch++
	active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, updated, nil, nil, tmux.Options{}, captureFn, changedHashFn)
	if active["ws-abc"] {
		t.Fatal("one changed scan (score=2) should not yet reach the threshold")
	}
	contentEpoch++
	active, _, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, updated, nil, nil, tmux.Options{}, captureFn, changedHashFn)
	if !active["ws-abc"] {
		t.Fatal("two changed scans should cross the threshold and mark the session active")
	}
}

func TestSessionActivityHysteresis(t *testing.T) {
	// Drives the real scan loop with a scripted hash sequence so the score
	// arithmetic (+2 per changed scan, -1 per unchanged scan, floor 0,
	// ceiling ScoreMax, threshold ScoreThreshold) is exercised against the
	// production pass, not restated as constants against constants.
	infoBySession := map[string]SessionInfo{
		"sess-hyst": {WorkspaceID: "ws-hyst", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "sess-hyst", WorkspaceID: "ws-hyst", Type: "agent"},
	}
	states := map[string]*SessionState{}

	// Scripted pane content: one hash value per scan. A value equal to the
	// previous scan's is an unchanged capture; a different one is a delta.
	// The tail repeats the climb's final hash so the decay phase starts
	// from a genuinely unchanged capture.
	script := [][16]byte{{1}, {2}, {3}, {3}, {3}, {4}, {5}, {6}, {6}, {6}, {6}, {6}, {6}, {6}, {6}, {6}}
	scan := 0
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte {
		h := script[scan%len(script)]
		scan++
		return h
	}

	scoreAt := func() int {
		st := states["sess-hyst"]
		if st == nil {
			return -1
		}
		return st.Score
	}
	// The scan reads the wall clock, so elapsed hold time cannot be
	// injected; aging the timestamp simulates HoldDuration passing.
	expireHold := func() {
		if st := states["sess-hyst"]; st != nil {
			st.LastActiveAt = time.Now().Add(-HoldDuration - time.Second)
		}
	}

	// Scan 1 (hash {1}): first observation seeds score 0, inactive.
	active, updated, _ := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	states = updated
	if active["ws-hyst"] || scoreAt() != 0 {
		t.Fatalf("first observation should seed score 0 and stay inactive, got active=%v score=%d", active["ws-hyst"], scoreAt())
	}

	// Scans 2-3 (hash {2}, {3}): deltas accumulate 0 -> 2 -> 4 — one
	// changed scan stays below the threshold, two cross it.
	active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	states = updated
	if active["ws-hyst"] || scoreAt() != 2 {
		t.Fatalf("one changed scan should stay below the threshold (score=2), got active=%v score=%d", active["ws-hyst"], scoreAt())
	}
	active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	states = updated
	if !active["ws-hyst"] || scoreAt() != 4 {
		t.Fatalf("two changed scans should cross the threshold (score=4, active), got active=%v score=%d", active["ws-hyst"], scoreAt())
	}

	// Scans 4-5 (hash {3}, {3}): with the hold window aged out, unchanged
	// captures decay 4 -> 3 -> 2 — the score at threshold stays active for
	// one decay, then drops below.
	expireHold()
	active, _, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	states = updated
	if !active["ws-hyst"] || scoreAt() != 3 {
		t.Fatalf("score at threshold should stay active after one decay, got active=%v score=%d", active["ws-hyst"], scoreAt())
	}
	active, _, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	states = updated
	if active["ws-hyst"] || scoreAt() != 2 {
		t.Fatalf("decayed score should drop below the threshold, got active=%v score=%d", active["ws-hyst"], scoreAt())
	}

	// Scans 6-8 (hash {4}, {5}, {6}): three more deltas climb back to the
	// ceiling — 2 -> 4 -> 6 (ScoreMax), still active.
	for i := 0; i < 3; i++ {
		active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
		states = updated
	}
	if !active["ws-hyst"] || scoreAt() != ScoreMax {
		t.Fatalf("consecutive changes should accumulate to max (score=%d), got active=%v score=%d", ScoreMax, active["ws-hyst"], scoreAt())
	}

	// Scans 9+ (hash {6} repeatedly): with the hold aged out, unchanged
	// captures decay to the floor — 6 -> 0 over seven scans, deactivating
	// as soon as the score drops below the threshold.
	expireHold()
	for i := 0; i < 8; i++ {
		active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
		states = updated
	}
	if active["ws-hyst"] || scoreAt() != 0 {
		t.Fatalf("unchanged captures should decay the score to 0 and deactivate, got active=%v score=%d", active["ws-hyst"], scoreAt())
	}
}
