// Package activity implements tmux session activity detection using
// screen-delta hysteresis and tag-based output timestamps. It is extracted
// from the app god-package to decouple pure detection logic from App state.
package activity

import (
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
)

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

// CaptureFn captures the tail of a tmux pane.
type CaptureFn func(sessionName string, lines int, opts tmux.Options) (string, bool)

// HashFn hashes pane content for delta detection.
type HashFn func(content string) [16]byte
