package app

import (
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// seedFreshTagFallbackBaseline initializes hysteresis state for sessions that
// are currently active via fresh tags, so stale fallback doesn't treat them as
// brand-new sessions and blip active on unchanged content.
func seedFreshTagFallbackBaseline(
	sessionName string,
	states map[string]*sessionActivityState,
	updated map[string]*sessionActivityState,
	opts tmux.Options,
	captureFn func(sessionName string, lines int, opts tmux.Options) (string, bool),
	hashFn func(content string) [16]byte,
) {
	if states == nil || updated == nil || strings.TrimSpace(sessionName) == "" {
		return
	}
	state := states[sessionName]
	if state != nil && state.initialized {
		return
	}
	if state == nil {
		state = &sessionActivityState{}
		states[sessionName] = state
	}
	if content, ok := captureFn(sessionName, activityCaptureTail, opts); ok {
		state.lastHash = hashFn(content)
	}
	state.initialized = true
	state.score = 0
	state.lastActiveAt = time.Time{}
	updated[sessionName] = state
}
