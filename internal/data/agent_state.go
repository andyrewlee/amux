package data

// AgentState is the semantic activity state of an agent session. It lives in
// this leaf package because every layer consumes it: the app computes it
// (internal/app/activity), tmux session tags serialize it, and the dashboard
// renders it — none of which may import the app package.
type AgentState int

const (
	StateIdle    AgentState = iota // quiet, nothing pending
	StateWorking                   // actively producing output
	StateDone                      // recently finished (was working, now quiet)
)

// String returns a human-readable label for the AgentState. These exact
// spellings are persisted in tmux session tags (@amux_agent_state).
func (s AgentState) String() string {
	switch s {
	case StateWorking:
		return "working"
	case StateDone:
		return "done"
	default:
		return "idle"
	}
}
