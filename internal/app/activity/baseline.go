package activity

import (
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// SeedFreshTagFallbackBaseline initializes hysteresis state for sessions that
// are currently active via fresh tags, so stale fallback doesn't treat them as
// brand-new sessions and blip active on unchanged content.
func SeedFreshTagFallbackBaseline(
	sessionName string,
	states map[string]*SessionState,
	updated map[string]*SessionState,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) {
	if states == nil || updated == nil || strings.TrimSpace(sessionName) == "" {
		return
	}
	state := states[sessionName]
	if state != nil && state.Initialized {
		return
	}
	if state == nil {
		state = &SessionState{}
		states[sessionName] = state
	}
	if content, ok := captureFn(sessionName, CaptureTail, opts); ok {
		state.LastHash = hashFn(content)
	}
	state.Initialized = true
	state.Score = 0
	state.LastActiveAt = time.Time{}
	updated[sessionName] = state
}

// FreshTagVisibleActivity validates fresh output tags against visible pane
// changes for initialized sessions. This prevents control-sequence churn from
// keeping sessions active when captured content is unchanged.
//
// Returns true when the fresh tag should count as active.
func FreshTagVisibleActivity(
	sessionName string,
	states map[string]*SessionState,
	updated map[string]*SessionState,
	now time.Time,
	opts tmux.Options,
	captureFn CaptureFn,
	hashFn HashFn,
) bool {
	if states == nil || updated == nil || strings.TrimSpace(sessionName) == "" {
		return true
	}

	state := states[sessionName]
	if state == nil || !state.Initialized {
		// First observation: trust fresh tag and seed fallback baseline.
		SeedFreshTagFallbackBaseline(sessionName, states, updated, opts, captureFn, hashFn)
		return true
	}

	content, ok := captureFn(sessionName, CaptureTail, opts)
	if !ok {
		// Capture is best-effort; keep fresh-tag behavior when unavailable.
		return true
	}

	hash := hashFn(content)
	if hash != state.LastHash {
		state.LastHash = hash
		// Returning true already marks this scan active; keep score at/above
		// threshold so a later known-session hysteresis pass does not restart
		// from zero.
		if state.Score < ScoreThreshold {
			state.Score = ScoreThreshold
		}
		state.LastActiveAt = now
		updated[sessionName] = state
		return true
	}

	// Fresh tag without visible pane delta: decay and clear hold.
	if state.Score > ScoreThreshold {
		state.Score = ScoreThreshold
	}
	if state.Score > 0 {
		state.Score--
	}
	state.LastActiveAt = time.Time{}
	updated[sessionName] = state
	return false
}
