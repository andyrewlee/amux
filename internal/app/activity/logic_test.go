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

	active, updated := ActiveWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)

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

func TestHysteresisNewSessionImmediatelyActive(t *testing.T) {
	// A newly discovered session with a successful capture should be
	// immediately active (score starts at threshold) without needing
	// multiple scan cycles.
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

	active, updated := ActiveWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)

	if !active["ws-abc"] {
		t.Fatal("newly discovered session with successful capture should be immediately active")
	}
	st := updated["amux-abc-tab-1"]
	if st == nil {
		t.Fatal("expected updated state for session")
	}
	if st.Score < ScoreThreshold {
		t.Fatalf("initial score should be >= threshold, got %d", st.Score)
	}
	if !st.Initialized {
		t.Fatal("state should be initialized after first capture")
	}
}

func TestSessionActivityHysteresis(t *testing.T) {
	state := &SessionState{}

	// Test 1: First observation sets score at threshold (immediately active)
	hash1 := [16]byte{1, 2, 3}
	state.LastHash = hash1
	state.Score = ScoreThreshold
	if state.Score < ScoreThreshold {
		t.Fatal("newly initialized session should be active at threshold")
	}

	// Test 2: Single change from zero should NOT reach threshold (threshold=3)
	state.Score = 0
	hash2 := [16]byte{4, 5, 6}
	state.Score += 2 // first change: score=2
	state.LastHash = hash2
	if state.Score >= ScoreThreshold {
		t.Fatalf("single change (score=%d) should NOT reach threshold %d", state.Score, ScoreThreshold)
	}

	// Test 3: Second consecutive change should reach threshold
	state.Score += 2 // second change: score=4
	if state.Score < ScoreThreshold {
		t.Fatalf("two consecutive changes (score=%d) should reach threshold %d", state.Score, ScoreThreshold)
	}

	// Test 4: No change decays score
	state.Score-- // score=3, still at threshold
	if state.Score < ScoreThreshold {
		t.Fatal("score should still be at threshold after one decay")
	}
	state.Score-- // score=2, below threshold
	if state.Score >= ScoreThreshold {
		t.Fatal("decayed score should be below threshold")
	}

	// Test 5: Multiple consecutive changes accumulate to max
	state.Score = 0
	state.Score += 2 // first change
	state.Score += 2 // second change
	state.Score += 2 // third change
	if state.Score > ScoreMax {
		state.Score = ScoreMax
	}
	if state.Score != ScoreMax {
		t.Fatalf("consecutive changes should accumulate to max (%d), got %d", ScoreMax, state.Score)
	}

	// Test 6: Decay from max
	for i := 0; i < 7; i++ {
		state.Score--
		if state.Score < 0 {
			state.Score = 0
		}
	}
	if state.Score != 0 {
		t.Fatalf("should decay to 0 after enough ticks without changes, got %d", state.Score)
	}
}

func TestActiveWorkspaceIDsFromTags_UsesTagWindowAndFallback(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: "sess-tag", WorkspaceID: "ws-tag", Type: "agent"},
			LastOutputAt:  now.Add(-time.Second),
			HasLastOutput: true,
		},
		{
			Session:       tmux.SessionActivity{Name: "sess-old", WorkspaceID: "ws-old", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
		{
			Session:       tmux.SessionActivity{Name: "sess-fallback", WorkspaceID: "ws-fallback", Type: "agent"},
			HasLastOutput: false,
		},
	}
	infoBySession := map[string]SessionInfo{
		// Keep sess-tag absent so the unknown-session fresh-tag path is exercised.
		"sess-old":      {WorkspaceID: "ws-old", IsChat: true},
		"sess-fallback": {WorkspaceID: "ws-fallback", IsChat: true},
	}
	states := map[string]*SessionState{}
	captureFn := func(sessionName string, _ int, _ tmux.Options) (string, bool) {
		if sessionName == "sess-fallback" {
			return "output", true
		}
		// Stale-tag session falls back, but capture failure should keep it inactive.
		return "", false
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{
		"sess-tag": true,
		"sess-old": true,
	}
	active, _ := ActiveWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)

	if !active["ws-tag"] {
		t.Fatal("expected ws-tag to be active from last-output tag")
	}
	if active["ws-old"] {
		t.Fatal("expected ws-old to be inactive when stale-tag fallback capture fails")
	}
	if !active["ws-fallback"] {
		t.Fatal("expected ws-fallback to be active via hysteresis fallback")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshTagWithoutRecentWindowActivityFallsBack(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-fresh-no-window"
	hashValue := [16]byte{9}
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh", Type: "agent"},
			LastOutputAt:  now.Add(-500 * time.Millisecond),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-fresh", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     hashValue,
			Score:        ScoreMax,
			LastActiveAt: now,
			Initialized:  true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-fresh"] {
		t.Fatal("expected fresh tag without recent window activity to fall back and remain inactive on unchanged content")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected fallback-updated state for fresh tag without recent window activity")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d, got %d", ScoreThreshold-1, state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected hold timer to be cleared when fresh tag falls back")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshTagWithoutRecentWindowActivity_DoesNotBlipWhenStateUninitialized(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-fresh-no-window-uninitialized"
	const workspaceID = "ws-fresh-no-window-uninitialized"
	hashValue := [16]byte{4}
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: workspaceID, Type: "agent"},
			LastOutputAt:  now.Add(-500 * time.Millisecond),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: workspaceID, IsChat: true},
	}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if active[workspaceID] {
		t.Fatal("expected no first-scan active blip for fresh tag without recent window activity when state is uninitialized")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected seeded fallback state for uninitialized fresh-tag session")
	}
	if !state.Initialized {
		t.Fatal("expected fallback state to be initialized")
	}
	if state.Score != 0 {
		t.Fatalf("expected seeded score to remain at 0 on unchanged fallback capture, got %d", state.Score)
	}
}

func TestActiveWorkspaceIDsFromTags_FreshTagActiveWhenPrefilterUnavailable(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: "sess-fresh", WorkspaceID: "ws-fresh", Type: "agent"},
			LastOutputAt:  now.Add(-500 * time.Millisecond),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, _ := ActiveWorkspaceIDsFromTags(infoBySession, sessions, nil, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-fresh"] {
		t.Fatal("expected fresh tag to remain active when prefilter is unavailable")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshTagWithRecentWindowButNoVisibleDeltaStaysInactive(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-fresh-no-delta"
	hashValue := [16]byte{5}
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh-no-delta", Type: "agent"},
			LastOutputAt:  now.Add(-400 * time.Millisecond),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-fresh-no-delta", IsChat: true},
	}
	// Hold timer is expired (beyond HoldDuration) so unchanged content
	// goes inactive immediately rather than being kept alive by the grace period.
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     hashValue,
			Score:        ScoreMax,
			LastActiveAt: now.Add(-HoldDuration - time.Second),
			Initialized:  true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	active, updated := ActiveWorkspaceIDsFromTags(
		infoBySession,
		sessions,
		map[string]bool{sessionName: true},
		states,
		tmux.Options{},
		captureFn,
		hashFn,
	)
	if active["ws-fresh-no-delta"] {
		t.Fatal("expected fresh tag with unchanged pane content to remain inactive")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected updated state for no-delta fresh tag")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d, got %d", ScoreThreshold-1, state.Score)
	}
}

func TestActiveWorkspaceIDsFromTags_StaleTagFallsBackToHysteresis(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: "sess-old", WorkspaceID: "ws-old", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		"sess-old": {WorkspaceID: "ws-old", IsChat: true},
	}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{
		"sess-old": true,
	}
	active, _ := ActiveWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-old"] {
		t.Fatal("expected ws-old to be active when stale-tag session shows live pane changes")
	}
}

func TestActiveWorkspaceIDsFromTags_StaleTagFallsBackWhenPrefilterUnavailable(t *testing.T) {
	now := time.Now()
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: "sess-stale", WorkspaceID: "ws-stale", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		"sess-stale": {WorkspaceID: "ws-stale", IsChat: true},
	}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, _ := ActiveWorkspaceIDsFromTags(infoBySession, sessions, nil, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-stale"] {
		t.Fatal("expected stale-tag session to fall back when prefilter is unavailable")
	}
}

func TestActiveWorkspaceIDsFromTags_KnownStaleTagFallsBackWithoutRecentActivity(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-known-stale"
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-tag", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-known", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {Score: ScoreMax, Initialized: true, LastActiveAt: now},
	}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "output", true
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	// Empty prefilter set should skip stale fallback to avoid idle capture churn.
	active, _ := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if captureCalls != 0 {
		t.Fatalf("expected no fallback capture without recent activity, got %d calls", captureCalls)
	}
	if active["ws-known"] {
		t.Fatal("expected known stale-tag session to stay inactive without recent prefilter activity")
	}
	if active["ws-stale-tag"] {
		t.Fatal("expected known metadata workspace ID to override stale tag workspace ID")
	}
	state := states[sessionName]
	if state == nil {
		t.Fatal("expected known stale-tag state to be preserved")
	}
	if state.Initialized != true {
		t.Fatal("expected known stale-tag baseline to stay initialized")
	}
	if state.Score != ScoreThreshold {
		t.Fatalf("expected known stale-tag score clamp to %d, got %d", ScoreThreshold, state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected known stale-tag hold timer to be cleared")
	}
}
