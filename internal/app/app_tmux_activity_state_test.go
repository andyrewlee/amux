package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestActiveWorkspaceIDsFromTags_StaleTagFallbackClearsHoldAndDecaysQuickly(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-stale-hold"
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-hold", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-stale-hold", IsChat: true},
	}
	states := map[string]*sessionActivityState{
		sessionName: {
			lastHash:     [16]byte{1},
			score:        activityScoreMax,
			lastActiveAt: now,
			initialized:  true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, updated := activeWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-stale-hold"] {
		t.Fatal("expected stale-tag unchanged session to stop being active without hold carryover")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected updated state for stale-tag session")
	}
	if state.score != activityScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after stale fallback clamp, got %d", activityScoreThreshold-1, state.score)
	}
	if !state.lastActiveAt.IsZero() {
		t.Fatal("expected stale fallback to clear hold timer")
	}
}

func TestActiveWorkspaceIDsFromTags_FreshOutputImmediatelyAfterInputSuppressed(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-echo"
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-echo", Type: "agent"},
			lastOutputAt:  now.Add(-100 * time.Millisecond),
			hasLastOutput: true,
			lastInputAt:   now.Add(-150 * time.Millisecond),
			hasLastInput:  true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-echo", IsChat: true},
	}
	states := map[string]*sessionActivityState{
		sessionName: {
			lastHash:     [16]byte{1},
			score:        activityScoreMax,
			lastActiveAt: now,
			initialized:  true,
		},
	}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "changed", true
	}
	hashFn := func(string) [16]byte { return [16]byte{2} }

	active, updated := activeWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
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
	if state.score != activityScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after suppression, got %d", activityScoreThreshold-1, state.score)
	}
	if !state.lastActiveAt.IsZero() {
		t.Fatal("expected suppression to clear hold timer")
	}
}

func TestActiveWorkspaceIDsFromTags_RecentInputSuppressesStaleFallbackCapture(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-recent-input"
	sessions := []taggedSessionActivity{
		{
			session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-recent-input", Type: "agent"},
			lastOutputAt:  now.Add(-10 * time.Second),
			hasLastOutput: true,
			lastInputAt:   now.Add(-500 * time.Millisecond),
			hasLastInput:  true,
		},
	}
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-recent-input", IsChat: true},
	}
	states := map[string]*sessionActivityState{
		sessionName: {
			lastHash:     [16]byte{1},
			score:        activityScoreMax,
			lastActiveAt: now,
			initialized:  true,
		},
	}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "changed", true
	}
	hashFn := func(string) [16]byte { return [16]byte{2} }

	active, updated := activeWorkspaceIDsFromTags(infoBySession, sessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
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
	if state.score != activityScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d after suppression, got %d", activityScoreThreshold-1, state.score)
	}
}

func TestHysteresisInitDoesNotSetHoldTimer(t *testing.T) {
	infoBySession := map[string]tabSessionInfo{
		"sess-init": {WorkspaceID: "ws-init", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "sess-init", WorkspaceID: "ws-init", Type: "agent"},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "output", true }
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, updated := activeWorkspaceIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-init"] {
		t.Fatal("expected newly discovered session to be active immediately")
	}
	state := updated["sess-init"]
	if state == nil {
		t.Fatal("expected session state to be initialized")
	}
	if !state.lastActiveAt.IsZero() {
		t.Fatal("expected initial observation to avoid hold timer")
	}

	// No further output; should decay below threshold on the next scan
	// without being held active.
	active, _ = activeWorkspaceIDsWithHysteresis(infoBySession, sessions, updated, tmux.Options{}, captureFn, hashFn)
	if active["ws-init"] {
		t.Fatal("expected session to stop being active after one unchanged scan")
	}
}

func TestActiveWorkspaceIDsFromTags_DoesNotResetFreshTagState(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-tagged"
	hashValue := [16]byte{1}
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-tagged", IsChat: true},
	}
	states := map[string]*sessionActivityState{
		sessionName: {
			lastHash:    hashValue,
			score:       0,
			initialized: true,
		},
	}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	// Scan 1: fresh tag path should mark active by tag but must not reset hysteresis state.
	freshSessions := []taggedSessionActivity{{
		session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-tagged", Type: "agent"},
		lastOutputAt:  now.Add(-500 * time.Millisecond),
		hasLastOutput: true,
	}}
	active, updated := activeWorkspaceIDsFromTags(infoBySession, freshSessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-tagged"] {
		t.Fatal("expected workspace to be active from fresh tag")
	}
	for name, state := range updated {
		states[name] = state
	}
	if !states[sessionName].initialized {
		t.Fatal("fresh-tag scan should not reset fallback hysteresis state")
	}

	// Scan 2: tag becomes stale; fallback should see unchanged content and remain inactive.
	staleSessions := []taggedSessionActivity{{
		session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-tagged", Type: "agent"},
		lastOutputAt:  now.Add(-10 * time.Second),
		hasLastOutput: true,
	}}
	active, updated = activeWorkspaceIDsFromTags(infoBySession, staleSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
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
	infoBySession := map[string]tabSessionInfo{
		sessionName: {WorkspaceID: "ws-fresh-seed", IsChat: true},
	}
	states := map[string]*sessionActivityState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	// Scan 1: fresh tag path marks active and should seed fallback baseline state.
	freshSessions := []taggedSessionActivity{{
		session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh-seed", Type: "agent"},
		lastOutputAt:  now.Add(-500 * time.Millisecond),
		hasLastOutput: true,
	}}
	active, updated := activeWorkspaceIDsFromTags(infoBySession, freshSessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws-fresh-seed"] {
		t.Fatal("expected workspace to be active from fresh tag")
	}
	state := updated[sessionName]
	if state == nil {
		t.Fatal("expected fresh-tag path to seed hysteresis state")
	}
	if !state.initialized {
		t.Fatal("expected seeded state to be initialized")
	}
	if state.score != 0 {
		t.Fatalf("expected seeded state score to start at 0, got %d", state.score)
	}
	for name, seeded := range updated {
		states[name] = seeded
	}

	// Scan 2: stale tag + unchanged pane must remain inactive (no fresh-session blip).
	staleSessions := []taggedSessionActivity{{
		session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-fresh-seed", Type: "agent"},
		lastOutputAt:  now.Add(-10 * time.Second),
		hasLastOutput: true,
	}}
	active, _ = activeWorkspaceIDsFromTags(infoBySession, staleSessions, map[string]bool{sessionName: true}, states, tmux.Options{}, captureFn, hashFn)
	if active["ws-fresh-seed"] {
		t.Fatal("expected stale fallback with unchanged content to stay inactive after fresh-tag seeding")
	}
}

func TestActiveWorkspaceIDsFromTags_StaleTagWithoutRecentActivitySkipsFallback(t *testing.T) {
	now := time.Now()
	const sessionName = "sess-stale-no-recent"
	infoBySession := map[string]tabSessionInfo{}
	states := map[string]*sessionActivityState{
		sessionName: {
			lastHash:    [16]byte{1},
			score:       activityScoreMax,
			initialized: true,
		},
	}
	sessions := []taggedSessionActivity{{
		session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: "ws-stale-no-recent", Type: "agent"},
		lastOutputAt:  now.Add(-10 * time.Second),
		hasLastOutput: true,
	}}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "output", true
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{}
	active, updated := activeWorkspaceIDsFromTags(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)

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
	if state.initialized || state.score != 0 {
		t.Fatalf("expected reset state for stale tag without recent activity, got initialized=%v score=%d", state.initialized, state.score)
	}
}

func TestScanTmuxActivityNow_QueuesWhenInFlight(t *testing.T) {
	app := &App{tmuxActivityScanInFlight: true}
	cmd := app.scanTmuxActivityNow()
	if cmd != nil {
		t.Fatal("expected nil cmd when scan already in flight")
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
}

func TestHandleTmuxActivityTick_QueuesWhenInFlight(t *testing.T) {
	app := &App{
		tmuxActivityToken:        7,
		tmuxAvailable:            true,
		tmuxActivityScanInFlight: true,
	}
	cmds := app.handleTmuxActivityTick(tmuxActivityTick{Token: 7})
	if len(cmds) != 1 {
		t.Fatalf("expected only ticker reschedule while in flight, got %d cmds", len(cmds))
	}
	if !app.tmuxActivityRescanPending {
		t.Fatal("expected pending rescan to be queued")
	}
	if app.tmuxActivityToken != 7 {
		t.Fatalf("expected token unchanged while in flight, got %d", app.tmuxActivityToken)
	}
}

func TestHandleTmuxActivityResult_ConsumesPendingRescan(t *testing.T) {
	app := &App{
		tmuxActivityToken:         2,
		tmuxAvailable:             true,
		tmuxActivityScanInFlight:  true,
		tmuxActivityRescanPending: true,
		sessionActivityStates:     make(map[string]*sessionActivityState),
		tmuxActiveWorkspaceIDs:    make(map[string]bool),
		dashboard:                 dashboard.New(),
	}
	cmds := app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              2,
		ActiveWorkspaceIDs: map[string]bool{},
		UpdatedStates:      map[string]*sessionActivityState{},
	})
	if len(cmds) == 0 {
		t.Fatal("expected pending rescan command to be enqueued")
	}
	if app.tmuxActivityToken != 3 {
		t.Fatalf("expected next scan token to be allocated, got %d", app.tmuxActivityToken)
	}
	if !app.tmuxActivityScanInFlight {
		t.Fatal("expected follow-up scan to be marked in flight")
	}
	if app.tmuxActivityRescanPending {
		t.Fatal("expected pending flag to be cleared")
	}
}
