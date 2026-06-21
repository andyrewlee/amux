// Package activity implements tmux session activity detection using
// screen-delta hysteresis and tag-based output timestamps. It is extracted
// from the app god-package to decouple pure detection logic from App state.
package activity

import (
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// Hysteresis / detection constants.
const (
	ScoreThreshold = 3 // Score needed to be considered active
	ScoreMax       = 6 // Maximum score (prevents runaway accumulation)

	// OutputWindow is how recently output must have occurred to be "active".
	OutputWindow = 2 * time.Second
	// InputEchoWindow treats output immediately after input as likely local echo.
	InputEchoWindow = 400 * time.Millisecond
	// InputSuppressWindow suppresses fallback capture right after user input.
	InputSuppressWindow = 2 * time.Second

	// CaptureTail is the number of terminal lines captured for activity checks.
	CaptureTail = 50
	// HoldDuration holds an active session state after the last observed change.
	HoldDuration = 6 * time.Second
	// DoneWindow is how long after going quiet a session is reported "done"
	// (recently finished) before decaying to idle.
	DoneWindow = 30 * time.Second
)

// AgentState is the semantic activity state of an agent session.
type AgentState int

const (
	StateIdle    AgentState = iota // quiet, nothing pending
	StateWorking                   // actively producing output
	StateDone                      // recently finished (was working, now quiet)
)

// String returns a human-readable label for the AgentState.
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

// SessionInfo maps a tmux session name to known tab metadata.
type SessionInfo struct {
	Status      string
	WorkspaceID string
	Assistant   string
	IsChat      bool
}

// SessionState tracks per-session activity using screen-delta hysteresis.
type SessionState struct {
	LastHash     [16]byte  // Hash of last captured pane content
	Score        int       // Activity score (0 to ScoreMax)
	LastActiveAt time.Time // Last time this session was considered active
	Initialized  bool      // Whether we have a baseline hash
	// UnseenScans counts consecutive scans in which this session was not seen.
	// Reset to 0 whenever the session is observed; once it exceeds
	// pruneAfterScans the state is dropped so deleted-workspace sessions do not
	// accumulate as zombie states that are deep-copied every scan forever.
	UnseenScans int
	// LastWorkingAt is the last time this session was classified active; used to
	// derive the transient "done" state after it goes quiet.
	LastWorkingAt time.Time
}

// TaggedSession pairs a tmux session with parsed tag timestamps.
type TaggedSession struct {
	Session       tmux.SessionActivity
	LastOutputAt  time.Time
	HasLastOutput bool
	LastInputAt   time.Time
	HasLastInput  bool
}

// SessionFetcher is the subset of tmux operations needed by activity detection.
type SessionFetcher interface {
	SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	ActiveAgentSessionsByActivity(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error)
}

// IsRunningSession reports whether a known session should be considered active-capable
// based on status metadata from app state.
func IsRunningSession(info SessionInfo, hasInfo bool) bool {
	if !hasInfo {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(info.Status))
	return status == "" || status == "running" || status == "detached"
}

// CaptureFn captures the tail of a tmux pane.
type CaptureFn func(sessionName string, lines int, opts tmux.Options) (string, bool)

// HashFn hashes pane content for delta detection.
type HashFn func(content string) [16]byte
