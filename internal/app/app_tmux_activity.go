package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
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
	// Prevents flicker from periodic terminal refreshes (e.g., sponsor
	// messages every ~30s). Newly discovered sessions start at the
	// threshold so they appear active immediately; if idle, the score
	// decays below threshold naturally.
	activityScoreThreshold = 3 // Score needed to be considered active
	activityScoreMax       = 6 // Maximum score (prevents runaway accumulation)

	// activityOutputWindow is how recently output must have occurred to be "active".
	activityOutputWindow = 2 * time.Second
	// activityInputEchoWindow treats output immediately after input as likely local echo.
	activityInputEchoWindow = 400 * time.Millisecond
	// activityInputSuppressWindow suppresses fallback capture right after user input.
	activityInputSuppressWindow = 2 * time.Second
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
	if a.tmuxActivityScanInFlight {
		a.tmuxActivityRescanPending = true
		return nil
	}
	a.tmuxActivityScanInFlight = true
	a.tmuxActivityRescanPending = false
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
		sessions, err := fetchTaggedSessions(svc, infoBySession, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		recentActivityBySession, err := fetchRecentlyActiveAgentSessionsByWindow(svc, opts)
		if err != nil {
			logging.Warn("tmux activity prefilter failed; using unbounded stale-tag fallback: %v", err)
			recentActivityBySession = nil
		}
		active, updatedStates := activeWorkspaceIDsFromTags(infoBySession, sessions, recentActivityBySession, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
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
	if a.tmuxActivityScanInFlight {
		a.tmuxActivityRescanPending = true
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	a.tmuxActivityScanInFlight = true
	a.tmuxActivityRescanPending = false
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
		sessions, err := fetchTaggedSessions(svc, sessionInfo, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		recentActivityBySession, err := fetchRecentlyActiveAgentSessionsByWindow(svc, opts)
		if err != nil {
			logging.Warn("tmux activity prefilter failed; using unbounded stale-tag fallback: %v", err)
			recentActivityBySession = nil
		}
		active, updatedStates := activeWorkspaceIDsFromTags(sessionInfo, sessions, recentActivityBySession, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
		return tmuxActivityResult{Token: scanToken, ActiveWorkspaceIDs: active, UpdatedStates: updatedStates}
	}}
	return cmds
}

func (a *App) handleTmuxActivityResult(msg tmuxActivityResult) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		// Stale result from an older scan; ignore to avoid overwriting newer state
		return nil
	}
	a.tmuxActivityScanInFlight = false
	var cmds []tea.Cmd
	if msg.Err != nil {
		logging.Warn("tmux activity scan failed: %v", msg.Err)
	} else {
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
	}
	if a.tmuxActivityRescanPending && a.tmuxAvailable {
		a.tmuxActivityRescanPending = false
		if scanCmd := a.scanTmuxActivityNow(); scanCmd != nil {
			cmds = append(cmds, scanCmd)
		}
	}
	return cmds
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	if !msg.available {
		return []tea.Cmd{a.toast.ShowError("tmux not installed. " + msg.installHint)}
	}
	cmds := []tea.Cmd{a.scanTmuxActivityNow()}
	if a.activeWorkspace != nil {
		if discoverCmd := a.discoverWorkspaceTabsFromTmux(a.activeWorkspace); discoverCmd != nil {
			cmds = append(cmds, discoverCmd)
		}
		if discoverTermCmd := a.discoverSidebarTerminalsFromTmux(a.activeWorkspace); discoverTermCmd != nil {
			cmds = append(cmds, discoverTermCmd)
		}
		if syncCmd := a.syncWorkspaceTabsFromTmux(a.activeWorkspace); syncCmd != nil {
			cmds = append(cmds, syncCmd)
		}
	}
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
