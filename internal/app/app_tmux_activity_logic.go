package app

import (
	"strconv"
	"strings"
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

// Concurrency safety: builds the map synchronously in the Update loop.
// Goroutine closures capture only the returned map, never accessing
// a.projects or ws.OpenTabs directly.
func (a *App) tabSessionInfoByName() map[string]tabSessionInfo {
	infoBySession := make(map[string]tabSessionInfo)
	assistants := map[string]struct{}{}
	if a.config != nil {
		for name := range a.config.Assistants {
			assistants[name] = struct{}{}
		}
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			for _, tab := range ws.OpenTabs {
				name := strings.TrimSpace(tab.SessionName)
				if name == "" {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(tab.Status))
				if status == "" {
					status = "running"
				}
				assistant := strings.TrimSpace(tab.Assistant)
				_, isChat := assistants[assistant]
				infoBySession[name] = tabSessionInfo{
					Status:      status,
					WorkspaceID: string(ws.ID()),
					Assistant:   assistant,
					IsChat:      isChat,
				}
			}
		}
	}
	return infoBySession
}

func fetchTaggedSessions(svc *tmuxService, infoBySession map[string]tabSessionInfo, opts tmux.Options) ([]taggedSessionActivity, error) {
	if svc == nil {
		return nil, errTmuxUnavailable
	}
	keys := []string{
		"@amux",
		"@amux_workspace",
		"@amux_tab",
		"@amux_type",
		tmux.TagLastOutputAt,
		tmux.TagLastInputAt,
	}
	rows, err := svc.SessionsWithTags(nil, keys, opts)
	if err != nil {
		return nil, err
	}
	sessions := make([]taggedSessionActivity, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		_, knownSession := infoBySession[name]
		amuxTag := strings.TrimSpace(row.Tags["@amux"])
		tagged := amuxTag != "" && amuxTag != "0"
		if !tagged && !knownSession && !looksLikeLegacyAmuxSession(name) {
			continue
		}
		session := tmux.SessionActivity{
			Name:        name,
			WorkspaceID: strings.TrimSpace(row.Tags["@amux_workspace"]),
			TabID:       strings.TrimSpace(row.Tags["@amux_tab"]),
			Type:        strings.TrimSpace(row.Tags["@amux_type"]),
			Tagged:      tagged,
		}
		lastOutputAt, ok := parseLastOutputAtTag(row.Tags[tmux.TagLastOutputAt])
		lastInputAt, hasInput := parseLastOutputAtTag(row.Tags[tmux.TagLastInputAt])
		sessions = append(sessions, taggedSessionActivity{
			session:       session,
			lastOutputAt:  lastOutputAt,
			hasLastOutput: ok,
			lastInputAt:   lastInputAt,
			hasLastInput:  hasInput,
		})
	}
	return sessions, nil
}

func fetchRecentlyActiveAgentSessionsByWindow(svc *tmuxService, opts tmux.Options) (map[string]bool, error) {
	if svc == nil {
		return nil, errTmuxUnavailable
	}
	sessions, err := svc.ActiveAgentSessionsByActivity(tmuxActivityPrefilter, opts)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		name := strings.TrimSpace(session.Name)
		if name == "" {
			continue
		}
		byName[name] = true
	}
	return byName, nil
}

func parseLastOutputAtTag(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return time.Time{}, false
	}
	switch {
	case parsed < 1_000_000_000_000:
		return time.Unix(parsed, 0), true
	case parsed < 1_000_000_000_000_000:
		return time.UnixMilli(parsed), true
	default:
		return time.Unix(0, parsed), true
	}
}

func looksLikeLegacyAmuxSession(name string) bool {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "amux-") {
		return false
	}
	if strings.Contains(name, "term-tab-") {
		return false
	}
	return strings.Contains(name, "-tab-")
}

func workspaceIDForSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) string {
	workspaceID := ""
	if hasInfo {
		workspaceID = strings.TrimSpace(info.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(session.WorkspaceID)
	}
	if workspaceID == "" {
		workspaceID = workspaceIDFromSessionName(session.Name)
	}
	return workspaceID
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

func shouldFallbackForStaleTag(sessionName string, recentActivityBySession map[string]bool) bool {
	name := strings.TrimSpace(sessionName)
	if name == "" {
		return false
	}
	// If prefilter data is unavailable, preserve behavior accuracy by allowing fallback.
	if recentActivityBySession == nil {
		return true
	}
	return recentActivityBySession[name]
}

func isLikelyUserEcho(snapshot taggedSessionActivity) bool {
	if !snapshot.hasLastInput || !snapshot.hasLastOutput {
		return false
	}
	if snapshot.lastOutputAt.Before(snapshot.lastInputAt) {
		return false
	}
	return snapshot.lastOutputAt.Sub(snapshot.lastInputAt) <= activityInputEchoWindow
}

func hasRecentUserInput(snapshot taggedSessionActivity, now time.Time) bool {
	if !snapshot.hasLastInput {
		return false
	}
	age := now.Sub(snapshot.lastInputAt)
	return age >= 0 && age <= activityInputSuppressWindow
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

// isChatSession determines whether a tmux session represents an active AI agent.
//
// Detection priority:
//  1. Known-tab metadata marks chat sessions active even if tmux type is stale.
//  2. Session tag (@amux_type == "agent") is authoritative for agent sessions.
//  3. For known sessions with no explicit type, fall back to tab metadata.
//  4. Name heuristic (legacy fallback) for pre-tag sessions.
func isChatSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) bool {
	if hasInfo && info.IsChat {
		return true
	}
	if session.Type != "" {
		return session.Type == "agent"
	}
	if hasInfo {
		return info.IsChat
	}
	// Legacy fallback for pre-tag sessions.
	name := session.Name
	if !strings.HasPrefix(name, "amux-") {
		return false
	}
	if strings.Contains(name, "term-tab-") {
		return false
	}
	return strings.Contains(name, "-tab-")
}
