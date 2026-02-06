package app

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type fetchTaggedSessionsOps struct {
	rows []tmux.SessionTagValues
	err  error
}

func (f fetchTaggedSessionsOps) EnsureAvailable() error { return nil }
func (f fetchTaggedSessionsOps) InstallHint() string    { return "" }
func (f fetchTaggedSessionsOps) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}
func (f fetchTaggedSessionsOps) SessionsWithTags(match map[string]string, keys []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
	if len(match) != 0 {
		return nil, errors.New("expected unfiltered SessionsWithTags call")
	}
	if len(keys) == 0 {
		return nil, errors.New("expected non-empty key list")
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}
func (f fetchTaggedSessionsOps) SessionStateFor(string, tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionState{}, nil
}
func (f fetchTaggedSessionsOps) SessionHasClients(string, tmux.Options) (bool, error) {
	return false, nil
}
func (f fetchTaggedSessionsOps) SessionCreatedAt(string, tmux.Options) (int64, error) { return 0, nil }
func (f fetchTaggedSessionsOps) KillSession(string, tmux.Options) error               { return nil }
func (f fetchTaggedSessionsOps) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	return false, nil
}
func (f fetchTaggedSessionsOps) KillSessionsWithPrefix(string, tmux.Options) error { return nil }
func (f fetchTaggedSessionsOps) KillWorkspaceSessions(string, tmux.Options) error  { return nil }
func (f fetchTaggedSessionsOps) SetMonitorActivityOn(tmux.Options) error           { return nil }
func (f fetchTaggedSessionsOps) SetStatusOff(tmux.Options) error                   { return nil }
func (f fetchTaggedSessionsOps) CapturePaneTail(string, int, tmux.Options) (string, bool) {
	return "", false
}
func (f fetchTaggedSessionsOps) ContentHash(string) [16]byte { return [16]byte{} }

func TestFetchTaggedSessions_IncludesKnownAndLegacySessionsWithoutAmuxTag(t *testing.T) {
	rows := []tmux.SessionTagValues{
		{
			Name: "amux-legacyws-tab-1",
			Tags: map[string]string{
				"@amux": "",
			},
		},
		{
			Name: "known-custom",
			Tags: map[string]string{
				"@amux": "",
			},
		},
		{
			Name: "amux-legacyws-term-tab-1",
			Tags: map[string]string{
				"@amux": "",
			},
		},
		{
			Name: "tagged-session",
			Tags: map[string]string{
				"@amux":              "1",
				"@amux_workspace":    "ws-tagged",
				"@amux_type":         "agent",
				tmux.TagLastOutputAt: "1700000000000",
				tmux.TagLastInputAt:  "1700000000000",
			},
		},
		{
			Name: "other-random",
			Tags: map[string]string{
				"@amux": "",
			},
		},
	}
	svc := newTmuxService(fetchTaggedSessionsOps{rows: rows})
	infoBySession := map[string]tabSessionInfo{
		"known-custom": {WorkspaceID: "ws-known", IsChat: true},
	}

	got, err := fetchTaggedSessions(svc, infoBySession, tmux.Options{})
	if err != nil {
		t.Fatalf("fetchTaggedSessions: %v", err)
	}

	byName := make(map[string]taggedSessionActivity, len(got))
	for _, session := range got {
		byName[session.session.Name] = session
	}

	if _, ok := byName["amux-legacyws-tab-1"]; !ok {
		t.Fatal("expected legacy amux tab session without @amux tag to be included")
	}
	if _, ok := byName["known-custom"]; !ok {
		t.Fatal("expected known session without @amux tag to be included")
	}
	if _, ok := byName["tagged-session"]; !ok {
		t.Fatal("expected tagged session to be included")
	}
	if _, ok := byName["amux-legacyws-term-tab-1"]; ok {
		t.Fatal("expected legacy term-tab session without @amux tag to be excluded")
	}
	if _, ok := byName["other-random"]; ok {
		t.Fatal("expected unrelated untagged session to be excluded")
	}

	if byName["tagged-session"].session.Tagged != true {
		t.Fatal("expected tagged session to preserve Tagged=true")
	}
	if byName["amux-legacyws-tab-1"].session.Tagged {
		t.Fatal("expected legacy untagged session to preserve Tagged=false")
	}
	if !byName["tagged-session"].hasLastOutput {
		t.Fatal("expected tagged session with timestamp tag to parse last output time")
	}
	if !byName["tagged-session"].hasLastInput {
		t.Fatal("expected tagged session with input tag to parse last input time")
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

	// Known tab metadata should win over stale/mismatched session type tags.
	session4 := tmux.SessionActivity{Name: "amux-ws1-tab-2", Type: "terminal"}
	if !isChatSession(session4, tabSessionInfo{IsChat: true}, true) {
		t.Fatal("known chat tab should be classified as chat even with stale type tag")
	}

	// Known tabs whose assistant metadata drifted should still honor tmux agent tags.
	session5 := tmux.SessionActivity{Name: "amux-ws1-tab-3", Type: "agent"}
	if !isChatSession(session5, tabSessionInfo{IsChat: false}, true) {
		t.Fatal("known session should still be chat when tmux type is explicitly agent")
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
	states := map[string]*sessionActivityState{
		"sess-direct":        warmState(),
		"sess-info-fallback": warmState(),
		"amux-ws99-tab-1":    warmState(),
		"sess-mismatch":      warmState(),
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
	infoBySession := map[string]tabSessionInfo{
		"amux-abc-tab-1": {WorkspaceID: "ws-abc", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "amux-abc-tab-1", WorkspaceID: "ws-abc", Type: "agent"},
	}
	// Empty states map — session has never been seen before
	states := map[string]*sessionActivityState{}

	captureFn := func(string, int, tmux.Options) (string, bool) { return "some output", true }
	hashFn := func(content string) [16]byte { return [16]byte{1} }

	active, updated := activeWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)

	if !active["ws-abc"] {
		t.Fatal("newly discovered session with successful capture should be immediately active")
	}
	st := updated["amux-abc-tab-1"]
	if st == nil {
		t.Fatal("expected updated state for session")
	}
	if st.score < activityScoreThreshold {
		t.Fatalf("initial score should be >= threshold, got %d", st.score)
	}
	if !st.initialized {
		t.Fatal("state should be initialized after first capture")
	}
}

func TestSessionActivityHysteresis(t *testing.T) {
	state := &sessionActivityState{}

	// Test 1: First observation sets score at threshold (immediately active)
	hash1 := [16]byte{1, 2, 3}
	state.lastHash = hash1
	state.initialized = true
	state.score = activityScoreThreshold
	if state.score < activityScoreThreshold {
		t.Fatal("newly initialized session should be active at threshold")
	}

	// Test 2: Single change from zero should NOT reach threshold (threshold=3)
	state.score = 0
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

func TestParseLastOutputAtTag(t *testing.T) {
	sec := int64(1_700_000_000)
	if got, ok := parseLastOutputAtTag("1700000000"); !ok || got.Unix() != sec {
		t.Fatalf("expected seconds parse to %d, got %v (ok=%v)", sec, got, ok)
	}
	ms := int64(1_700_000_000_000)
	if got, ok := parseLastOutputAtTag("1700000000000"); !ok || got.UnixMilli() != ms {
		t.Fatalf("expected millis parse to %d, got %v (ok=%v)", ms, got, ok)
	}
	ns := int64(1_700_000_000_000_000_000)
	if got, ok := parseLastOutputAtTag("1700000000000000000"); !ok || got.UnixNano() != ns {
		t.Fatalf("expected nanos parse to %d, got %v (ok=%v)", ns, got, ok)
	}
	if _, ok := parseLastOutputAtTag(""); ok {
		t.Fatal("expected empty value to be invalid")
	}
	if _, ok := parseLastOutputAtTag("0"); ok {
		t.Fatal("expected zero to be invalid")
	}
}

func TestActiveWorkspaceIDsFromTags_UsesTagWindowAndFallback(t *testing.T) {
	now := time.Now()
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: "sess-tag", WorkspaceID: "ws-tag", Type: "agent"},
			lastOutputAt:  now.Add(-time.Second),
			hasLastOutput: true,
		},
		{
			session:       tmux.SessionActivity{Name: "sess-old", WorkspaceID: "ws-old", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
		},
		{
			session:       tmux.SessionActivity{Name: "sess-fallback", WorkspaceID: "ws-fallback", Type: "agent"},
			hasLastOutput: false,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		"sess-tag":      {WorkspaceID: "ws-tag", IsChat: true},
		"sess-old":      {WorkspaceID: "ws-old", IsChat: true},
		"sess-fallback": {WorkspaceID: "ws-fallback", IsChat: true},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(sessionName string, _ int, _ tmux.Options) (string, bool) {
		if sessionName == "sess-fallback" {
			return "output", true
		}
		// Stale-tag session falls back, but capture failure should keep it inactive.
		return "", false
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{
		"sess-old": true,
	}
	active, _ := activeWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)

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

func TestActiveWorkspaceIDsFromTags_StaleTagFallsBackToHysteresis(t *testing.T) {
	now := time.Now()
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: "sess-old", WorkspaceID: "ws-old", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		"sess-old": {WorkspaceID: "ws-old", IsChat: true},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{
		"sess-old": true,
	}
	active, _ := activeWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-old"] {
		t.Fatal("expected ws-old to be active when stale-tag session shows live pane changes")
	}
}

func TestActiveWorkspaceIDsFromTags_StaleTagFallsBackWhenPrefilterUnavailable(t *testing.T) {
	now := time.Now()
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: "sess-stale", WorkspaceID: "ws-stale", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		"sess-stale": {WorkspaceID: "ws-stale", IsChat: true},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, _ := activeWorkspaceIDsFromTags(infoBySession, sessions, nil, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-stale"] {
		t.Fatal("expected stale-tag session to fall back when prefilter is unavailable")
	}
}

func TestActiveWorkspaceIDsFromTags_KnownStaleTagFallsBackWithoutRecentActivity(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-known-stale"
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-tag", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-known", IsChat: true},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	// Empty prefilter set should not block known-session stale fallback.
	active, _ := activeWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-known"] {
		t.Fatal("expected known stale-tag session to remain eligible for fallback without recent prefilter activity")
	}
	if active["ws-stale-tag"] {
		t.Fatal("expected known metadata workspace ID to override stale tag workspace ID")
	}
}
