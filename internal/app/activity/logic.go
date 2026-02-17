package activity

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// ActiveWorkspaceIDsFromTags uses the @amux_last_output_at tag when present.
// Sessions with missing tags always fall back to screen-delta hysteresis
// (compatibility mode). Sessions with stale tags fall back when they have
// recent tmux window activity (or if that prefilter is unavailable).
func ActiveWorkspaceIDsFromTags(
	infoBySession map[string]SessionInfo,
	sessions []TaggedSession,
	recentActivityBySession map[string]bool,
	states map[string]*SessionState,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState) {
	active := make(map[string]bool)
	var fallback []tmux.SessionActivity
	suppressedByInput := make(map[string]bool)
	preseededStates := make(map[string]*SessionState)
	seenChatSessions := make(map[string]bool, len(sessions))
	now := time.Now()

	for _, snapshot := range sessions {
		info, ok := infoBySession[snapshot.Session.Name]
		if !IsChatSession(snapshot.Session, info, ok) {
			continue
		}
		if snapshot.HasLastOutput {
			age := now.Sub(snapshot.LastOutputAt)
			if age >= 0 && age <= OutputWindow {
				if IsLikelyUserEcho(snapshot) {
					PrepareStaleTagFallbackState(snapshot.Session.Name, states)
					suppressedByInput[snapshot.Session.Name] = true
					seenChatSessions[snapshot.Session.Name] = true
					fallback = append(fallback, snapshot.Session)
					continue
				}
				SeedFreshTagFallbackBaseline(snapshot.Session.Name, states, preseededStates, opts, captureFn, hashFn)
				seenChatSessions[snapshot.Session.Name] = true
				if workspaceID := WorkspaceIDForSession(snapshot.Session, info, ok); workspaceID != "" {
					active[workspaceID] = true
				}
				continue
			}
			// Future-dated tags are suspicious (clock skew or stale writes);
			// fall back to pane-delta for safety.
			if age < 0 {
				PrepareStaleTagFallbackState(snapshot.Session.Name, states)
				seenChatSessions[snapshot.Session.Name] = true
				fallback = append(fallback, snapshot.Session)
				continue
			}
			if HasRecentUserInput(snapshot, now) {
				PrepareStaleTagFallbackState(snapshot.Session.Name, states)
				suppressedByInput[snapshot.Session.Name] = true
				seenChatSessions[snapshot.Session.Name] = true
				fallback = append(fallback, snapshot.Session)
				continue
			}
			// For known tabs, always keep pane-delta fallback enabled.
			// Their metadata is authoritative and detached sessions may
			// not refresh @amux_last_output_at.
			if ok {
				PrepareStaleTagFallbackState(snapshot.Session.Name, states)
				seenChatSessions[snapshot.Session.Name] = true
				fallback = append(fallback, snapshot.Session)
				continue
			}
			// Stale-tag fallback is gated by recent tmux activity to avoid
			// capture-pane work on long-idle sessions each scan.
			if ShouldFallbackForStaleTag(snapshot.Session.Name, recentActivityBySession) {
				PrepareStaleTagFallbackState(snapshot.Session.Name, states)
				seenChatSessions[snapshot.Session.Name] = true
				fallback = append(fallback, snapshot.Session)
			}
			continue
		}
		if HasRecentUserInput(snapshot, now) {
			PrepareStaleTagFallbackState(snapshot.Session.Name, states)
			suppressedByInput[snapshot.Session.Name] = true
			seenChatSessions[snapshot.Session.Name] = true
			fallback = append(fallback, snapshot.Session)
			continue
		}
		seenChatSessions[snapshot.Session.Name] = true
		fallback = append(fallback, snapshot.Session)
	}

	captureWithSuppression := captureFn
	if len(suppressedByInput) > 0 {
		captureWithSuppression = func(sessionName string, lines int, opts tmux.Options) (string, bool) {
			if suppressedByInput[sessionName] {
				return "", false
			}
			return captureFn(sessionName, lines, opts)
		}
	}
	fallbackActive, updated := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, fallback, states, seenChatSessions, opts, captureWithSuppression, hashFn)
	for name, state := range preseededStates {
		updated[name] = state
	}
	for workspaceID := range fallbackActive {
		active[workspaceID] = true
	}
	return active, updated
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

// ActiveWorkspaceIDsWithHysteresis uses screen-delta detection with hysteresis
// to determine which workspaces have actively working agents. This prevents
// false positives from periodic terminal refreshes (like sponsor messages).
// Returns both the active workspace IDs and the updated session states.
func ActiveWorkspaceIDsWithHysteresis(
	infoBySession map[string]SessionInfo,
	sessions []tmux.SessionActivity,
	states map[string]*SessionState,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState) {
	return activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, opts, captureFn, hashFn)
}

func activeWorkspaceIDsWithHysteresisWithSeen(
	infoBySession map[string]SessionInfo,
	sessions []tmux.SessionActivity,
	states map[string]*SessionState,
	seenSessions map[string]bool,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) (map[string]bool, map[string]*SessionState) {
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
			workspaceID := WorkspaceIDForSession(session, info, ok)
			if workspaceID != "" {
				active[workspaceID] = true
			}
		}
	}

	// Decay/reset states for sessions not seen in this scan.
	// This prevents stale scores from persisting when a session falls out of
	// the prefilter window (>120s idle) and then reappears with a single refresh.
	for name, state := range states {
		if seenSessions[name] {
			continue // Already processed above
		}
		// Reset score and baseline so stale hashes/hold timers don't trigger
		// false positives when a session re-enters the prefilter window.
		state.Score = 0
		state.LastActiveAt = time.Time{}
		state.Initialized = false
		state.LastHash = [16]byte{}
		updatedStates[name] = state
	}

	return active, updatedStates
}
