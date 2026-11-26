package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

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
	active, _, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)

	if !active["ws-tag"] {
		t.Fatal("expected ws-tag to be active from last-output tag")
	}
	if active["ws-old"] {
		t.Fatal("expected ws-old to be inactive when stale-tag fallback capture fails")
	}
	if active["ws-fallback"] {
		t.Fatal("expected ws-fallback to seed its baseline without claiming activity: a session with no output tag carries no freshness evidence")
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

	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
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

	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
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

	active, _, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, nil, states, tmux.Options{}, captureFn, hashFn)
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

	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(
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
	const sessionName = "sess-old"
	const workspaceID = "ws-old"
	sessions := []TaggedSession{
		{
			Session:       tmux.SessionActivity{Name: sessionName, WorkspaceID: workspaceID, Type: "agent"},
			LastOutputAt:  now.Add(-10 * time.Second),
			HasLastOutput: true,
		},
	}
	infoBySession := map[string]SessionInfo{
		sessionName: {WorkspaceID: workspaceID, IsChat: true},
	}
	// Empty states map: the session is first-observed on this scan, exactly
	// like an app start or an activity-scan ownership handoff leaves behind.
	states := map[string]*SessionState{}

	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "output", true
	}
	// Static content: the pane is not producing output.
	hashFn := func(string) [16]byte { return [16]byte{1} }

	recentActivity := map[string]bool{
		sessionName: true,
	}
	active, updated, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, recentActivity, states, tmux.Options{}, captureFn, hashFn)
	if captureCalls == 0 {
		t.Fatal("expected stale-tag session with recent window activity to fall back to a capture")
	}
	if active[workspaceID] {
		t.Fatal("expected stale-tag session with unchanged pane content to stay inactive on first observation")
	}
	if st := updated[sessionName]; st == nil || !st.Initialized || st.Score != 0 {
		t.Fatalf("expected first observation to seed an initialized zero-score baseline, got %+v", updated[sessionName])
	}

	// The pane starts producing output: deltas accumulate across scans and
	// the session crosses the threshold — fallback works, activity just
	// requires evidence now.
	contentEpoch := 0
	changedHashFn := func(string) [16]byte { return [16]byte{byte(2 + contentEpoch)} }
	contentEpoch++
	active, updated, _ = ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, recentActivity, updated, tmux.Options{}, captureFn, changedHashFn)
	if active[workspaceID] {
		t.Fatal("one changed scan (score=2) should not yet reach the threshold")
	}
	contentEpoch++
	active, _, _ = ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, recentActivity, updated, tmux.Options{}, captureFn, changedHashFn)
	if !active[workspaceID] {
		t.Fatal("expected stale-tag session to become active once pane deltas cross the threshold")
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
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "output", true
	}
	hashFn := func(string) [16]byte { return [16]byte{1} }

	active, _, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, nil, states, tmux.Options{}, captureFn, hashFn)
	if captureCalls == 0 {
		t.Fatal("expected stale-tag session to fall back when prefilter is unavailable")
	}
	if active["ws-stale"] {
		t.Fatal("expected stale-tag session to stay inactive on first observation despite the fallback capture")
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
	active, _, _ := ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, map[string]bool{}, states, tmux.Options{}, captureFn, hashFn)
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
