package data

// AgentState is the semantic activity state of an agent session. It lives in
// this leaf package because every layer consumes it: the app computes it
// (internal/app/activity), tmux session tags serialize it, and the dashboard
// renders it — none of which may import the app package.
//
// Idle/Working/Done are heuristic-derived (screen-output hysteresis).
// Blocked/Error are reporter-derived only: they exist solely when a program
// self-reported that state through the Program Status Protocol (OSC 7501 or
// `amux report`). amux never infers them from the screen.
type AgentState int

const (
	StateIdle    AgentState = iota // quiet, nothing pending
	StateWorking                   // actively producing output
	StateDone                      // recently finished (was working, now quiet)
	StateBlocked                   // reported: waiting on the user
	StateError                     // reported: ended in failure
)

// String returns a human-readable label for the AgentState. These exact
// spellings are persisted in tmux session tags (@amux_agent_state) and match
// the protocol's reported-state labels (internal/agentstatus).
func (s AgentState) String() string {
	switch s {
	case StateWorking:
		return "working"
	case StateDone:
		return "done"
	case StateBlocked:
		return "blocked"
	case StateError:
		return "error"
	default:
		return "idle"
	}
}

// ParseAgentState parses a persisted or reported state label. Unknown labels
// (the protocol's custom states) return ok=false; callers keep the label
// verbatim for display and fall back to the heuristic classification for
// the enum.
func ParseAgentState(s string) (AgentState, bool) {
	switch s {
	case "idle":
		return StateIdle, true
	case "working":
		return StateWorking, true
	case "done":
		return StateDone, true
	case "blocked":
		return StateBlocked, true
	case "error":
		return StateError, true
	default:
		return StateIdle, false
	}
}
