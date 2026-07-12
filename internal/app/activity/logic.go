package activity

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// ActiveWorkspaceIDsFromTagsWithRemoved is the production entry point. It
// returns the active workspace IDs, updated session states, and the names of
// session states pruned this scan (unseen beyond pruneAfterScans) so the caller
// can delete them from its persistent map.
func ActiveWorkspaceIDsFromTagsWithRemoved(
	infoBySession map[string]SessionInfo,
	sessions []TaggedSession,
	recentActivityBySession map[string]bool,
	states map[string]*SessionState,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState, []string) {
	return activeWorkspaceIDsFromTags(infoBySession, sessions, recentActivityBySession, states, opts, captureFn, hashFn)
}

// activeWorkspaceIDsFromTags uses the @amux_last_output_at tag when present.
// Sessions with missing tags always fall back to screen-delta hysteresis
// (compatibility mode). Sessions with stale tags fall back when they have
// recent tmux window activity (or if that prefilter is unavailable).
// Fresh tags are trusted only when tmux reports recent window activity
// (or if that prefilter is unavailable), preventing control-sequence noise
// from holding sessions in an always-active state.
func activeWorkspaceIDsFromTags(
	infoBySession map[string]SessionInfo,
	sessions []TaggedSession,
	recentActivityBySession map[string]bool,
	states map[string]*SessionState,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState, []string) {
	c := &tagClassifier{
		infoBySession:           infoBySession,
		recentActivityBySession: recentActivityBySession,
		states:                  states,
		opts:                    opts,
		captureFn:               captureFn,
		hashFn:                  hashFn,
		now:                     time.Now(),
		active:                  make(map[string]bool),
		suppressedByInput:       make(map[string]bool),
		preseededStates:         make(map[string]*SessionState),
		seenChatSessions:        make(map[string]bool, len(sessions)),
	}
	for _, snapshot := range sessions {
		c.classify(snapshot)
	}

	captureWithSuppression := captureFn
	if len(c.suppressedByInput) > 0 {
		captureWithSuppression = func(sessionName string, lines int, opts tmux.Options) (string, bool) {
			if c.suppressedByInput[sessionName] {
				return "", false
			}
			return captureFn(sessionName, lines, opts)
		}
	}
	fallbackActive, updated, removed := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, c.fallback, states, c.seenChatSessions, opts, captureWithSuppression, hashFn)
	// preseededStates entries point at the same *SessionState objects in
	// states/updated; this assignment preserves updates when fallback skipped
	// the session in this scan.
	for name, state := range c.preseededStates {
		updated[name] = state
	}
	for workspaceID := range fallbackActive {
		c.active[workspaceID] = true
	}
	return c.active, updated, removed
}

// tagClassifier accumulates the per-session routing decisions made while reading
// output tags: which sessions to push through pane-delta hysteresis (fallback),
// which to suppress capture for (recent input), preseeded baselines, the set
// seen this scan, and any workspaces already proven active by a visible delta.
type tagClassifier struct {
	infoBySession           map[string]SessionInfo
	recentActivityBySession map[string]bool
	states                  map[string]*SessionState
	opts                    tmux.Options
	captureFn               CaptureFn
	hashFn                  HashFn
	now                     time.Time

	active            map[string]bool
	fallback          []tmux.SessionActivity
	suppressedByInput map[string]bool
	preseededStates   map[string]*SessionState
	seenChatSessions  map[string]bool
}

func (c *tagClassifier) markSeenFallback(name string, session tmux.SessionActivity) {
	c.seenChatSessions[name] = true
	c.fallback = append(c.fallback, session)
}

// classify routes a single tagged session into the accumulators.
func (c *tagClassifier) classify(snapshot TaggedSession) {
	name := snapshot.Session.Name
	info, ok := c.infoBySession[name]
	if !IsChatSession(snapshot.Session, info, ok) {
		return
	}
	if !IsRunningSession(info, ok) {
		return
	}
	if !snapshot.HasLastOutput {
		if HasRecentUserInput(snapshot, c.now) {
			PrepareStaleTagFallbackState(name, c.states)
			c.suppressedByInput[name] = true
			c.markSeenFallback(name, snapshot.Session)
			return
		}
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	age := c.now.Sub(snapshot.LastOutputAt)
	if age >= 0 && age <= OutputWindow {
		c.classifyFreshOutput(snapshot, info, ok)
		return
	}
	c.classifyStaleOutput(snapshot, age, ok)
}

// classifyFreshOutput handles sessions whose output tag is within OutputWindow.
func (c *tagClassifier) classifyFreshOutput(snapshot TaggedSession, info SessionInfo, ok bool) {
	name := snapshot.Session.Name
	if IsLikelyUserEcho(snapshot) {
		PrepareStaleTagFallbackState(name, c.states)
		c.suppressedByInput[name] = true
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	// Fresh output tags without recent tmux window activity are often
	// control-sequence churn (no visible pane delta). Route these through
	// hysteresis fallback instead of immediate active.
	if !HasRecentWindowActivity(name, c.recentActivityBySession) {
		PrepareStaleTagFallbackState(name, c.states)
		// Seeds baseline hash (calls capture-pane for uninitialized states);
		// hysteresis will capture again — acceptable cost.
		SeedFreshTagFallbackBaseline(name, c.states, c.preseededStates, c.opts, c.captureFn, c.hashFn)
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	// Known tabs are evaluated via pane-delta hysteresis even when tags are
	// fresh, which avoids persistent "active" false positives from non-meaningful
	// tag churn.
	//
	// Behavioral note: unlike stale-tag fallback (which clears the hold timer via
	// PrepareStaleTagFallbackState), this path preserves it. A session recently
	// above threshold stays active for HoldDuration even if the next hysteresis
	// capture fails or shows unchanged content, preventing a single transient
	// failure from immediately deactivating a known active tab.
	if ok {
		// Cap score only; SeedFreshTagFallbackBaseline resets score for
		// uninitialized states anyway, so capping is a no-op there.
		if state := c.states[name]; state != nil && state.Score > ScoreThreshold {
			state.Score = ScoreThreshold
		}
		// Note: for uninitialized states this calls capture-pane to seed a
		// baseline hash; hysteresis will call it again. The double capture is a
		// minor cost limited to first observation.
		SeedFreshTagFallbackBaseline(name, c.states, c.preseededStates, c.opts, c.captureFn, c.hashFn)
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	// Unknown sessions that fail visible-delta validation are intentionally
	// skipped from fallback; FreshTagVisibleActivity already decayed/updated state.
	if !FreshTagVisibleActivity(name, c.states, c.preseededStates, c.now, c.opts, c.captureFn, c.hashFn) {
		c.seenChatSessions[name] = true
		return
	}
	c.seenChatSessions[name] = true
	if workspaceID := WorkspaceIDForSession(snapshot.Session, info, ok); workspaceID != "" {
		if st := c.states[name]; st != nil {
			st.LastWorkingAt = c.now
		}
		c.active[workspaceID] = true
	}
}

// classifyStaleOutput handles sessions whose output tag is older than
// OutputWindow (or future-dated), routing them to pane-delta fallback when
// warranted.
func (c *tagClassifier) classifyStaleOutput(snapshot TaggedSession, age time.Duration, ok bool) {
	name := snapshot.Session.Name
	// Future-dated tags are suspicious (clock skew or stale writes); fall back to
	// pane-delta for safety.
	if age < 0 {
		PrepareStaleTagFallbackState(name, c.states)
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	if HasRecentUserInput(snapshot, c.now) {
		PrepareStaleTagFallbackState(name, c.states)
		c.suppressedByInput[name] = true
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	// Stale-tag fallback is gated by recent tmux activity to avoid capture-pane
	// work on long-idle sessions each scan.
	if HasRecentWindowActivity(name, c.recentActivityBySession) {
		PrepareStaleTagFallbackState(name, c.states)
		c.markSeenFallback(name, snapshot.Session)
		return
	}
	if ok {
		// Known sessions were observed in this scan but intentionally skipped for
		// expensive fallback capture. Mark them seen so we preserve hysteresis
		// state instead of hard-resetting it.
		PrepareStaleTagFallbackState(name, c.states)
		c.seenChatSessions[name] = true
	}
}

// PrepareStaleTagFallbackState trims stale-tag hysteresis carryover so sessions
// stop appearing active promptly after output ceases.
func PrepareStaleTagFallbackState(sessionName string, states map[string]*SessionState) {
	if states == nil {
		return
	}
	state := states[sessionName]
	if state == nil {
		return
	}
	if state.Score > ScoreThreshold {
		state.Score = ScoreThreshold
	}
	// Disable hold extension for stale-tag fallback; rely on live pane deltas instead.
	state.LastActiveAt = time.Time{}
}

// activeWorkspaceIDsWithHysteresisWithSeen uses screen-delta detection with
// hysteresis to determine which workspaces have actively working agents. This
// prevents false positives from periodic terminal refreshes (like sponsor
// messages).
func activeWorkspaceIDsWithHysteresisWithSeen(
	infoBySession map[string]SessionInfo,
	sessions []tmux.SessionActivity,
	states map[string]*SessionState,
	seenSessions map[string]bool,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState, []string) {
	active := make(map[string]bool)
	updatedStates := make(map[string]*SessionState)
	now := time.Now()

	// Track which sessions we see in this scan.
	if seenSessions == nil {
		seenSessions = make(map[string]bool, len(sessions))
	}

	for _, session := range sessions {
		seenSessions[session.Name] = true
		info, ok := infoBySession[session.Name]
		if !IsChatSession(session, info, ok) {
			continue
		}

		// Get or create state for this session
		state := states[session.Name]
		if state == nil {
			state = &SessionState{}
		}
		// Observed this scan: it is not a candidate for unseen-pruning.
		state.UnseenScans = 0
		observedWork := false

		// Capture pane content and compute hash
		content, captureOK := captureFn(session.Name, CaptureTail, opts)
		if captureOK {
			hash := hashFn(content)

			// Update hysteresis score based on content change
			if !state.Initialized {
				// First time seeing this session -- treat as active immediately.
				// If it stops generating output, hysteresis decay will clear it
				// on the next scan without triggering hold duration.
				state.LastHash = hash
				state.Initialized = true
				state.Score = ScoreThreshold
			} else if hash != state.LastHash {
				// Content changed - bump score
				state.Score += 2
				if state.Score > ScoreMax {
					state.Score = ScoreMax
				}
				state.LastHash = hash
				// Only update LastActiveAt when crossing the active threshold,
				// so hold duration doesn't apply to single changes below threshold
				if state.Score >= ScoreThreshold {
					state.LastActiveAt = now
					observedWork = true
				}
			} else {
				// No change - decay score
				state.Score--
				if state.Score < 0 {
					state.Score = 0
				}
			}
		} else {
			// Capture failed - decay score to prevent stale "active" states
			// from persisting when capture keeps failing
			state.Score--
			if state.Score < 0 {
				state.Score = 0
			}
		}

		// Track updated state for merging back on main thread
		updatedStates[session.Name] = state

		// Determine if session is active based on score and hold duration
		isActive := state.Score >= ScoreThreshold
		if !isActive && !state.LastActiveAt.IsZero() {
			// Check hold duration - stay active for a bit after last change
			if now.Sub(state.LastActiveAt) < HoldDuration {
				isActive = true
			}
		}

		if isActive {
			if observedWork {
				state.LastWorkingAt = now
			}
			workspaceID := WorkspaceIDForSession(session, info, ok)
			if workspaceID != "" {
				active[workspaceID] = true
			}
		}
	}

	// Decay/reset states for sessions not seen in this scan, and prune ones that
	// have been unseen long enough that they are almost certainly gone (e.g. a
	// deleted workspace's session that will never reappear in the scan).
	removed := decayOrPruneUnseenStates(states, seenSessions, updatedStates)

	return active, updatedStates, removed
}

// pruneAfterScans is how many consecutive unseen scans a session state survives
// (still reset-and-retained so re-entry stays correct) before it is pruned.
// At the ~5s scan cadence this is ~15s of absence.
const pruneAfterScans = 3

// decayOrPruneUnseenStates resets the hysteresis of sessions not seen this scan
// (preserving re-entry correctness) until they have been unseen for more than
// pruneAfterScans, after which they are dropped: omitted from updatedStates and
// returned in the removed slice so the caller can delete them. Seen sessions
// have their unseen counter reset (re-emitted only when it actually changed).
func decayOrPruneUnseenStates(
	states map[string]*SessionState,
	seenSessions map[string]bool,
	updatedStates map[string]*SessionState,
) []string {
	var removed []string
	for name, state := range states {
		if seenSessions[name] {
			// Seen via a path that skipped the main loop (e.g. tag fallback that
			// did not capture). Make sure it is not counted toward pruning.
			if state.UnseenScans != 0 {
				state.UnseenScans = 0
				updatedStates[name] = state
			}
			continue
		}
		state.UnseenScans++
		if state.UnseenScans > pruneAfterScans {
			// Drop entirely: do not emit, signal removal so the caller deletes it.
			removed = append(removed, name)
			continue
		}
		// Reset score and baseline so stale hashes/hold timers don't trigger
		// false positives when a session re-enters the prefilter window. Keep the
		// incremented UnseenScans so the prune window keeps counting down.
		state.Score = 0
		state.LastActiveAt = time.Time{}
		state.Initialized = false
		state.LastHash = [16]byte{}
		updatedStates[name] = state
	}
	return removed
}
