package app

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type tabSessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
}

type taggedSessionActivity struct {
	session       tmux.SessionActivity
	lastOutputAt  time.Time
	hasLastOutput bool
	lastInputAt   time.Time
	hasLastInput  bool
}

// activeWorkspaceIDsFromTags uses the @amux_last_output_at tag when present.
// Sessions with missing tags always fall back to screen-delta hysteresis
// (compatibility mode). Sessions with stale tags fall back when they have
// recent tmux window activity (or if that prefilter is unavailable).
func activeWorkspaceIDsFromTags(
	infoBySession map[string]tabSessionInfo,
	sessions []taggedSessionActivity,
	recentActivityBySession map[string]bool,
	states map[string]*sessionActivityState,
	opts tmux.Options,
	captureFn func(sessionName string, lines int, opts tmux.Options) (string, bool),
	hashFn func(content string) [16]byte,
) (map[string]bool, map[string]*sessionActivityState) {
	active := make(map[string]bool)
	var fallback []tmux.SessionActivity
	suppressedByInput := make(map[string]bool)
	preseededStates := make(map[string]*sessionActivityState)
	seenChatSessions := make(map[string]bool, len(sessions))
	now := time.Now()

	for _, snapshot := range sessions {
		info, ok := infoBySession[snapshot.session.Name]
		if !isChatSession(snapshot.session, info, ok) {
			continue
		}
		if snapshot.hasLastOutput {
			age := now.Sub(snapshot.lastOutputAt)
			if age >= 0 && age <= activityOutputWindow {
				if isLikelyUserEcho(snapshot) {
					prepareStaleTagFallbackState(snapshot.session.Name, states)
					suppressedByInput[snapshot.session.Name] = true
					seenChatSessions[snapshot.session.Name] = true
					fallback = append(fallback, snapshot.session)
					continue
				}
				seedFreshTagFallbackBaseline(snapshot.session.Name, states, preseededStates, opts, captureFn, hashFn)
				seenChatSessions[snapshot.session.Name] = true
				if workspaceID := workspaceIDForSession(snapshot.session, info, ok); workspaceID != "" {
					active[workspaceID] = true
				}
				continue
			}
			// Future-dated tags are suspicious (clock skew or stale writes);
			// fall back to pane-delta for safety.
			if age < 0 {
				prepareStaleTagFallbackState(snapshot.session.Name, states)
				seenChatSessions[snapshot.session.Name] = true
				fallback = append(fallback, snapshot.session)
				continue
			}
			if hasRecentUserInput(snapshot, now) {
				prepareStaleTagFallbackState(snapshot.session.Name, states)
				suppressedByInput[snapshot.session.Name] = true
				seenChatSessions[snapshot.session.Name] = true
				fallback = append(fallback, snapshot.session)
				continue
			}
			// For known tabs, always keep pane-delta fallback enabled.
			// Their metadata is authoritative and detached sessions may
			// not refresh @amux_last_output_at.
			if ok {
				prepareStaleTagFallbackState(snapshot.session.Name, states)
				seenChatSessions[snapshot.session.Name] = true
				fallback = append(fallback, snapshot.session)
				continue
			}
			// Stale-tag fallback is gated by recent tmux activity to avoid
			// capture-pane work on long-idle sessions each scan.
			if shouldFallbackForStaleTag(snapshot.session.Name, recentActivityBySession) {
				prepareStaleTagFallbackState(snapshot.session.Name, states)
				seenChatSessions[snapshot.session.Name] = true
				fallback = append(fallback, snapshot.session)
			}
			continue
		}
		if hasRecentUserInput(snapshot, now) {
			prepareStaleTagFallbackState(snapshot.session.Name, states)
			suppressedByInput[snapshot.session.Name] = true
			seenChatSessions[snapshot.session.Name] = true
			fallback = append(fallback, snapshot.session)
			continue
		}
		seenChatSessions[snapshot.session.Name] = true
		fallback = append(fallback, snapshot.session)
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

// prepareStaleTagFallbackState trims stale-tag hysteresis carryover so sessions
// stop appearing active promptly after output ceases.
func prepareStaleTagFallbackState(sessionName string, states map[string]*sessionActivityState) {
	if states == nil {
		return
	}
	state := states[sessionName]
	if state == nil {
		return
	}
	if state.score > activityScoreThreshold {
		state.score = activityScoreThreshold
	}
	// Disable hold extension for stale-tag fallback; rely on live pane deltas instead.
	state.lastActiveAt = time.Time{}
}

// activeWorkspaceIDsWithHysteresis uses screen-delta detection with hysteresis
// to determine which workspaces have actively working agents. This prevents
// false positives from periodic terminal refreshes (like sponsor messages).
// Returns both the active workspace IDs and the updated session states.
func activeWorkspaceIDsWithHysteresis(
	infoBySession map[string]tabSessionInfo,
	sessions []tmux.SessionActivity,
	states map[string]*sessionActivityState,
	opts tmux.Options,
	captureFn func(sessionName string, lines int, opts tmux.Options) (string, bool),
	hashFn func(content string) [16]byte,
) (map[string]bool, map[string]*sessionActivityState) {
	return activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, opts, captureFn, hashFn)
}

func activeWorkspaceIDsWithHysteresisWithSeen(
	infoBySession map[string]tabSessionInfo,
	sessions []tmux.SessionActivity,
	states map[string]*sessionActivityState,
	seenSessions map[string]bool,
	opts tmux.Options,
	captureFn func(sessionName string, lines int, opts tmux.Options) (string, bool),
	hashFn func(content string) [16]byte,
) (map[string]bool, map[string]*sessionActivityState) {
	active := make(map[string]bool)
	updatedStates := make(map[string]*sessionActivityState)
	now := time.Now()

	// Track which sessions we see in this scan.
	if seenSessions == nil {
		seenSessions = make(map[string]bool, len(sessions))
	}

	for _, session := range sessions {
		seenSessions[session.Name] = true
		info, ok := infoBySession[session.Name]
		if !isChatSession(session, info, ok) {
			continue
		}

		// Get or create state for this session
		state := states[session.Name]
		if state == nil {
			state = &sessionActivityState{}
		}

		// Capture pane content and compute hash
		content, captureOK := captureFn(session.Name, activityCaptureTail, opts)
		if captureOK {
			hash := hashFn(content)

			// Update hysteresis score based on content change
			if !state.initialized {
				// First time seeing this session — treat as active immediately.
				// If it stops generating output, hysteresis decay will clear it
				// on the next scan without triggering hold duration.
				state.lastHash = hash
				state.initialized = true
				state.score = activityScoreThreshold
			} else if hash != state.lastHash {
				// Content changed - bump score
				state.score += 2
				if state.score > activityScoreMax {
					state.score = activityScoreMax
				}
				state.lastHash = hash
				// Only update lastActiveAt when crossing the active threshold,
				// so hold duration doesn't apply to single changes below threshold
				if state.score >= activityScoreThreshold {
					state.lastActiveAt = now
				}
			} else {
				// No change - decay score
				state.score--
				if state.score < 0 {
					state.score = 0
				}
			}
		} else {
			// Capture failed - decay score to prevent stale "active" states
			// from persisting when capture keeps failing
			state.score--
			if state.score < 0 {
				state.score = 0
			}
		}

		// Track updated state for merging back on main thread
		updatedStates[session.Name] = state

		// Determine if session is active based on score and hold duration
		isActive := state.score >= activityScoreThreshold
		if !isActive && !state.lastActiveAt.IsZero() {
			// Check hold duration - stay active for a bit after last change
			if now.Sub(state.lastActiveAt) < activityHoldDuration {
				isActive = true
			}
		}

		if isActive {
			workspaceID := workspaceIDForSession(session, info, ok)
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
		state.score = 0
		state.lastActiveAt = time.Time{}
		state.initialized = false
		state.lastHash = [16]byte{}
		updatedStates[name] = state
	}

	return active, updatedStates
}
