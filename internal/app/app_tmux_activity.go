package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type tmuxActivityTick struct {
	Token int
}

type tmuxActivityResult struct {
	Token              int
	ActiveWorkspaceIDs map[string]bool
	UpdatedStates      map[string]*sessionActivityState // Updated hysteresis states to merge
	Err                error
}

const (
	// Hysteresis thresholds for screen-delta activity detection.
	// Requires sustained changes to become active, prevents flicker from
	// periodic terminal refreshes (e.g., sponsor messages every ~30s).
	// With increment=2 and threshold=3, at least 2 consecutive changes are
	// needed to reach active state (first change: 2, second change: 4 >= 3).
	activityScoreThreshold = 3 // Score needed to be considered active
	activityScoreMax       = 6 // Maximum score (prevents runaway accumulation)
)

// sessionActivityState tracks per-session activity using screen-delta hysteresis.
type sessionActivityState struct {
	lastHash     [16]byte  // Hash of last captured pane content
	score        int       // Activity score (0 to activityScoreMax)
	lastActiveAt time.Time // Last time this session was considered active
	initialized  bool      // Whether we have a baseline hash
}

// snapshotActivityStates creates a deep copy of session activity states for use in a goroutine.
// This avoids concurrent map access between the Update loop and Cmd goroutines.
func (a *App) snapshotActivityStates() map[string]*sessionActivityState {
	snapshot := make(map[string]*sessionActivityState, len(a.sessionActivityStates))
	for name, state := range a.sessionActivityStates {
		// Copy the struct to avoid sharing pointers
		stateCopy := *state
		snapshot[name] = &stateCopy
	}
	return snapshot
}

func (a *App) startTmuxActivityTicker() tea.Cmd {
	a.tmuxActivityToken++
	return a.scheduleTmuxActivityTick()
}

func (a *App) scheduleTmuxActivityTick() tea.Cmd {
	token := a.tmuxActivityToken
	return common.SafeTick(tmuxActivityInterval, func(time.Time) tea.Msg {
		return tmuxActivityTick{Token: token}
	})
}

func (a *App) triggerTmuxActivityScan() tea.Cmd {
	token := a.tmuxActivityToken
	return func() tea.Msg {
		return tmuxActivityTick{Token: token}
	}
}

func (a *App) scanTmuxActivityNow() tea.Cmd {
	a.tmuxActivityToken++
	scanToken := a.tmuxActivityToken
	infoBySession := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxActivityResult{Token: scanToken, Err: errTmuxUnavailable}
		}
		sessions, err := svc.ActiveAgentSessionsByActivity(tmuxActivityPrefilter, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		active, updatedStates := activeWorkspaceIDsWithHysteresis(infoBySession, sessions, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
		return tmuxActivityResult{Token: scanToken, ActiveWorkspaceIDs: active, UpdatedStates: updatedStates}
	}
}

func (a *App) handleTmuxActivityTick(msg tmuxActivityTick) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	if !a.tmuxAvailable {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	// Increment token for this scan so out-of-order results are rejected.
	// Each scan gets a unique token; only the most recent result is applied.
	a.tmuxActivityToken++
	scanToken := a.tmuxActivityToken
	sessionInfo := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	cmds := []tea.Cmd{a.scheduleTmuxActivityTick(), func() tea.Msg {
		if svc == nil {
			return tmuxActivityResult{Token: scanToken, Err: errTmuxUnavailable}
		}
		sessions, err := svc.ActiveAgentSessionsByActivity(tmuxActivityPrefilter, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		active, updatedStates := activeWorkspaceIDsWithHysteresis(sessionInfo, sessions, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
		return tmuxActivityResult{Token: scanToken, ActiveWorkspaceIDs: active, UpdatedStates: updatedStates}
	}}
	return cmds
}

func (a *App) handleTmuxActivityResult(msg tmuxActivityResult) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		// Stale result from an older scan; ignore to avoid overwriting newer state
		return nil
	}
	var cmds []tea.Cmd
	if msg.Err != nil {
		logging.Warn("tmux activity scan failed: %v", msg.Err)
		return cmds
	}
	if msg.ActiveWorkspaceIDs == nil {
		msg.ActiveWorkspaceIDs = make(map[string]bool)
	}
	// Merge updated hysteresis states back into the main map (on main thread)
	for name, state := range msg.UpdatedStates {
		a.sessionActivityStates[name] = state
	}
	a.tmuxActiveWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.syncActiveWorkspacesToDashboard()
	if cmd := a.dashboard.StartSpinnerIfNeeded(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

type tabSessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
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
	active := make(map[string]bool)
	updatedStates := make(map[string]*sessionActivityState)
	now := time.Now()

	// Track which sessions we see in this scan
	seenSessions := make(map[string]bool, len(sessions))

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
				// First time seeing this session, just record baseline
				state.lastHash = hash
				state.initialized = true
				state.score = 0
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
			workspaceID := strings.TrimSpace(session.WorkspaceID)
			if workspaceID == "" && ok {
				workspaceID = strings.TrimSpace(info.WorkspaceID)
			}
			if workspaceID == "" {
				workspaceID = workspaceIDFromSessionName(session.Name)
			}
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
//  1. Session tag (@amux_type == "agent") — authoritative, set at creation time.
//  2. Stored tab metadata (info.IsChat) — from assistant config lookup.
//  3. Name heuristic (legacy fallback) — matches "amux-*-tab-*" sessions,
//     excluding terminal tabs ("term-tab-"). Only used for sessions tagged
//     with @amux but missing @amux_type (older versions), to avoid false
//     positives from unrelated tmux sessions.
func isChatSession(session tmux.SessionActivity, info tabSessionInfo, hasInfo bool) bool {
	if session.Type != "" {
		return session.Type == "agent"
	}
	if hasInfo {
		return info.IsChat
	}
	if !session.Tagged {
		return false
	}
	// Legacy fallback for untagged sessions (pre-tagging era).
	name := session.Name
	if !strings.HasPrefix(name, "amux-") {
		return false
	}
	if strings.Contains(name, "term-tab-") {
		return false
	}
	return strings.Contains(name, "-tab-")
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	if !msg.available {
		return []tea.Cmd{a.toast.ShowError("tmux not installed. " + msg.installHint)}
	}
	cmds := []tea.Cmd{a.scanTmuxActivityNow()}
	if a.tmuxService != nil {
		cmds = append(cmds, func() tea.Msg {
			_ = a.tmuxService.SetMonitorActivityOn(a.tmuxOptions)
			_ = a.tmuxService.SetStatusOff(a.tmuxOptions)
			return nil
		})
	}
	return cmds
}

// resetAllTabStatuses marks all non-stopped tabs as stopped and schedules
// persistence for changed workspaces. Used when switching tmux servers so
// the UI doesn't show stale running/detached status.
func (a *App) resetAllTabStatuses() []tea.Cmd {
	var cmds []tea.Cmd
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			changed := false
			for k := range ws.OpenTabs {
				if ws.OpenTabs[k].Status != "" && ws.OpenTabs[k].Status != "stopped" {
					ws.OpenTabs[k].Status = "stopped"
					changed = true
				}
			}
			if changed {
				cmds = append(cmds, a.persistWorkspaceTabs(string(ws.ID())))
			}
		}
	}
	return cmds
}

func workspaceIDFromSessionName(name string) string {
	const prefix = "amux-"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	trimmed := strings.TrimPrefix(name, prefix)
	parts := strings.Split(trimmed, "-")
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}
