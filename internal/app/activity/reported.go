package activity

import (
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
	"github.com/andyrewlee/amux/internal/data"
)

// The reported-status merge: sessions whose program self-reports through the
// Program Status Protocol (internal/agentstatus) carry authoritative state —
// amux stores what was reported and never re-derives it from the screen.
// Sessions without a report keep the heuristic classification untouched.
// The merge is stateless per scan: report presence in the pane-option read
// IS the authority bit, and the pane's lifecycle (report options die with
// the pane) is the report's lifecycle, so no local bookkeeping can drift.

// ReportedSeverity orders states for the workspace fold: a blocked agent
// needs the user now, an error almost as urgently, then working, done,
// idle. It is agentstatus.SeverityOf over the state's wire label — one
// ranking table serves the pane fold, the workspace fold, and everything
// downstream, so they can never disagree.
func ReportedSeverity(s data.AgentState) int {
	return agentstatus.SeverityOf(s.String())
}

// MergeReportedSessionStates returns the effective semantic state of every
// retained session: the report when one exists (known labels only), else the
// heuristic classification at `now`. This is the single classification
// source for both the per-session @amux_agent_state publish and the
// workspace fold, so the reported and heuristic views of one session can
// never disagree within a scan.
//
// The protocol's process-exit lifetime is enforced here: working and
// blocked records die with the pane's process (a dead pane's pipe may
// never deliver EOF to the tap, so the read marks such reports and this
// merge drops them), while done, error, and idle records survive it.
func MergeReportedSessionStates(
	states map[string]*SessionState,
	reports map[string]agentstatus.Report,
	now time.Time,
) map[string]data.AgentState {
	out := make(map[string]data.AgentState, len(states))
	for name, st := range states {
		out[name] = ClassifyState(st, now)
		if r, ok := reports[name]; ok {
			if r.PaneDead && (r.State == agentstatus.StateWorking || r.State == agentstatus.StateBlocked) {
				continue // process-exit lifetime: transient states are void
			}
			// Known labels only: a legacy or hand-written option value with
			// an unknown state must not clobber the heuristic view.
			if mapped, known := data.ParseAgentState(r.State); known {
				out[name] = mapped
			}
		}
	}
	return out
}

// FoldWorkspaceStates folds per-session semantic states into the per-
// workspace map. The heuristic active bit seeds Working (unchanged
// behavior for reporters and non-reporters alike), then every session's
// semantic state — reported or heuristic — competes by severity: a blocked
// report outranks the active bit, a reported idle never masks it. The
// result omits nothing an all-idle workspace needs: absent and Idle read
// identically downstream (AgentState's zero value).
func FoldWorkspaceStates(
	active map[string]bool,
	semantics map[string]data.AgentState,
	workspaceBySession map[string]string,
) map[string]data.AgentState {
	out := make(map[string]data.AgentState, len(active))
	for wsID := range active {
		out[wsID] = data.StateWorking
	}
	for name, st := range semantics {
		wsID := workspaceBySession[name]
		if wsID == "" {
			continue
		}
		if prev, exists := out[wsID]; !exists || ReportedSeverity(st) > ReportedSeverity(prev) {
			out[wsID] = st
		}
	}
	return out
}
