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
