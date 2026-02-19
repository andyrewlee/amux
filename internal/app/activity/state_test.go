package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestActiveWorkspaceIDsFromTags_StaleTagFallbackClearsHoldAndDecaysQuickly(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-stale-hold"
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-hold", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-stale-hold", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     [16]byte{1},
			Score:        ScoreMax,
			LastActiveAt: now,
			Initialized:  true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-stale-hold"] {
		t.Fatal("expected stale-tag unchanged session to stop being active without hold carryover")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected updated state for stale-tag session")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after stale fallback clamp, got %d", ScoreThreshold-1, state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected stale fallback to clear hold timer")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshOutputImmediatelyAfterInputSuppressed(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-echo"
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-echo", Type: "agent"},
			LastOutputAt:  now.Add(-100 * time.Millisecond),
			HasLastOutput: true,
			LastInputAt:   now.Add(-150 * time.Millisecond),
			HasLastInput:  true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-echo", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     [16]byte{1},
			Score:        ScoreMax,
			LastActiveAt: now,
			Initialized:  true,
		},
	}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "changed", true
	}
	hashFn := func(string) [16]byte { return [16]byte{2} }

	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-echo"] {
		t.Fatal("fresh output immediately after input should be treated as local echo, not activity")
	}
	if captureCalls != 0 {
		t.Fatalf("expected suppressed echo path to skip capture-pane, got %d calls", captureCalls)
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected updated state for suppressed echo session")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after suppression, got %d", ScoreThreshold-1, state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected suppression to clear hold timer")
	}
}

func TestActiveWorkspaceIDsFromTags_RecentInputSuppressesStaleFallbackCapture(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-recent-input"
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-recent-input", Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
			LastInputAt:   now.Add(-500 * time.Millisecond),
			HasLastInput:  true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-recent-input", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     [16]byte{1},
			Score:        ScoreMax,
			LastActiveAt: now,
			Initialized:  true,
		},
	}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "changed", true
	}
	hashFn := func(string) [16]byte { return [16]byte{2} }

	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-recent-input"] {
		t.Fatal("stale-tag fallback should be suppressed while user input is recent")
	}
	if captureCalls != 0 {
		t.Fatalf("expected recent-input suppression to skip capture-pane, got %d calls", captureCalls)
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected updated state for recent-input suppression")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after suppression, got %d", ScoreThreshold-1, state.Score)
	}
}

func TestHysteresisInitDoesNotSetHoldTimer(t *testing.T) {
	infoBySession := map[string]SessionInfo{
		"sess-init": {WorkspaceID: "ws-init", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "sess-init", WorkspaceID: "ws-init", Type: "agent"},
	}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, updated := ActiveWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-init"] {
		t.Fatal("expected newly discovered session to be active immediately")
	}
	state := updated["sess-init"]
	if state == nil {
		t.Fatal("expected session state to be initialized")
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected initial observation to avoid hold timer")
	}

	// No further output; should decay below threshold on the next scan
	// without being held active.
	active, _ = ActiveWorkspaceIDsWithHysteresis(infoBySession, sessions, updated, tmux.Options{}, captureFn, hashFn)
	if active["ws-init"] {
		t.Fatal("expected session to stop being active after one unchanged scan")
	}
}

func TestActiveWorkspaceIDsFromTags_DoesNotResetFreshTagState(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-tagged"
	hashValue := [16]byte{1}
	changedHash := [16]byte{2}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-tagged", IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:    hashValue,
			Score:       0,
			Initialized: true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	currentHash := changedHash
	hashFn := func(string) [16]byte { return currentHash }

	// Scan 1: known fresh-tag sessions flow through hysteresis and should
	// not blip active on a single visible change.
	freshSessions := []TaggedSession{{
		Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-tagged", Type: "agent"},
		LastOutputAt:  now.Add(-500 * time.Millisecond),
		HasLastOutput: true,
	}}
	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, freshSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-tagged"] {
		t.Fatal("expected single fresh-tag delta for known session to remain below active threshold")
	}
	for name, state := range updated {
		states[name] = state
	}
	if !states[sessionName].Initialized {
		t.Fatal("fresh-tag scan should not reset fallback hysteresis state")
	}
	if states[sessionName].Score != 2 {
		t.Fatalf("expected score 2 after one known-session fresh delta, got %d", states[sessionName].Score)
	}

	// Scan 2: tag becomes stale; fallback should see unchanged content and remain inactive.
	staleSessions := []TaggedSession{{
		Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-tagged", Type: "agent"},
		LastOutputAt:  now.Add(-10 * time.Second),
		HasLastOutput: true,
	}}
	currentHash = changedHash
	active, updated = ActiveWorkspaceIDsFromTags(infoBySession, staleSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	for name, state := range updated {
		states[name] = state
	}
	if active["ws-tagged"] {
		t.Fatal("stale-tag fallback should not blip active when content is unchanged")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshTagSeedsFallbackBaseline(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-fresh-seed"
	hashValue := [16]byte{7}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: "ws-fresh-seed", IsChat: true},
	}
	states := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	// Scan 1: known fresh-tag session should seed baseline without an
	// immediate active blip.
	freshSessions := []TaggedSession{{
		Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh-seed", Type: "agent"},
		LastOutputAt:  now.Add(-500 * time.Millisecond),
		HasLastOutput: true,
	}}
	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, freshSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-fresh-seed"] {
		t.Fatal("expected known fresh-tag baseline seeding to remain inactive")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected fresh-tag path to seed hysteresis state")
	}
	if !state.Initialized {
		t.Fatal("expected seeded state to be initialized")
	}
	if state.Score != 0 {
		t.Fatalf("expected seeded state score to start at 0, got %d", state.Score)
	}
	for name, seeded := range updated {
		states[name] = seeded
	}

	// Scan 2: stale tag + unchanged pane must remain inactive (no fresh-session blip).
	staleSessions := []TaggedSession{{
		Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh-seed", Type: "agent"},
		LastOutputAt:  now.Add(-10 * time.Second),
		HasLastOutput: true,
	}}
	active, _ = ActiveWorkspaceIDsFromTags(infoBySession, staleSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-fresh-seed"] {
		t.Fatal("expected stale fallback with unchanged content to stay inactive after fresh-tag seeding")
	}
}

func TestActiveWorkspaceIDsFromTags_StaleTagWithoutRecentActivitySkipsFallback(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-stale-no-recent"
	infoBySession := map[string]SessionInfo{}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:    [16]byte{1},
			Score:       ScoreMax,
			Initialized: true,
		},
	}
	sessions := []TaggedSession{{
		Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-no-recent", Type: "agent"},
		LastOutputAt:  now.Add(-10 * time.Second),
		HasLastOutput: true,
	}}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "output", true
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{}
	active, updated := ActiveWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)

	if captureCalls != 0 {
		t.Fatalf("expected no capture fallback without recent activity, got %d calls", captureCalls)
	}
	if active["ws-stale-no-recent"] {
		t.Fatal("stale tagged session without recent activity should not be marked active")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected stale tagged session state to be reset")
	}
	if state.Initialized || state.Score != 0 {
		t.Fatalf("expected reset state for stale tag without recent activity, got initialized=%v score=%d", state.Initialized, state.Score)
	}
}

func TestActiveWorkspaceIDsFromTags_KnownFreshUnchangedAcrossScansRemainInactive(t *testing.T) {
	const sessionName = "sess-known-fresh-unchanged"
	const workspaceID = "ws-known-fresh-unchanged"
	hashValue := [16]byte{4}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: workspaceID, IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:     hashValue,
			Score:        ScoreMax,
			LastActiveAt: time.Now(),
			Initialized:  true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	for i := 0; i < 5; i++ {
		sessions := []TaggedSession{{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: workspaceID, Type: "agent"},
			LastOutputAt:  time.Now().Add(-500 * time.Millisecond),
			HasLastOutput: true,
		}}
		active, updated := ActiveWorkspaceIDsFromTags(
			infoBySession,
			sessions,
			map[string]bool{sessionName: true},
			states,
			tmux.Options{},
			captureFn,
			hashFn,
		)
		if active[workspaceID] {
			t.Fatalf("scan %d: expected unchanged known fresh-tag session to stay inactive", i+1)
		}
		for name, state := range updated {
			states[name] = state
		}
	}

	state := states[sessionName]
	if state == nil {
		t.Fatal("expected state to remain tracked")
	}
	if state.Score != 0 {
		t.Fatalf("expected score to decay to 0 after repeated unchanged scans, got %d", state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected hold timer to be cleared for unchanged known fresh-tag session")
	}
}

func TestActiveWorkspaceIDsFromTags_KnownFreshConsecutiveDeltasBecomeActive(t *testing.T) {
	const sessionName = "sess-known-fresh-deltas"
	const workspaceID = "ws-known-fresh-deltas"
	oldHash := [16]byte{1}
	hashSeq := [][16]byte{{2}, {3}}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: workspaceID, IsChat: true},
	}
	states := map[string]*SessionState{
		sessionName: {
			LastHash:    oldHash,
			Score:       0,
			Initialized: true,
		},
	}
	currentHash := hashSeq[0]
	captureFn := func(string, int, tmux.Options) (string, bool) { return "changed", true }
	hashFn := func(string) [16]byte { return currentHash }

	for i := 0; i < len(hashSeq); i++ {
		currentHash = hashSeq[i]
		sessions := []TaggedSession{{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: workspaceID, Type: "agent"},
			LastOutputAt:  time.Now().Add(-400 * time.Millisecond),
			HasLastOutput: true,
		}}
		active, updated := ActiveWorkspaceIDsFromTags(
			infoBySession,
			sessions,
			map[string]bool{sessionName: true},
			states,
			tmux.Options{},
			captureFn,
			hashFn,
		)
		for name, state := range updated {
			states[name] = state
		}

		if i == 0 && active[workspaceID] {
			t.Fatal("expected first visible delta to remain below active threshold")
		}
		if i == 1 && !active[workspaceID] {
			t.Fatal("expected second visible delta to cross active threshold")
		}
	}
}
