package common

import "time"

// SelectionScrollTickInterval is the interval between auto-scroll ticks
// during mouse-drag selection past viewport edges.
const SelectionScrollTickInterval = 100 * time.Millisecond

// SelectionScrollState is the shared state machine for tick-based
// auto-scrolling during mouse-drag text selection. Both the sidebar
// terminal and the center pane embed this struct.
type SelectionScrollState struct {
	// Gen is a generation counter used to invalidate stale tick loops.
	Gen uint64
	// ScrollDir is +1 (scroll up into history) or -1 (scroll down toward
	// live output) or 0 (no auto-scroll).
	ScrollDir int
	// Active is true when a tick loop is currently running.
	Active bool
}

// SetDirection updates ScrollDir based on the unclamped terminal Y
// coordinate. Call this before clamping termY to [0, termHeight).
func (s *SelectionScrollState) SetDirection(termY, termHeight int) {
	if termY < 0 {
		s.ScrollDir = 1 // scroll up into history
	} else if termY >= termHeight {
		s.ScrollDir = -1 // scroll down toward live output
	} else {
		s.ScrollDir = 0
	}
}

// NeedsTick returns (true, gen) when a new tick loop should be started.
// It bumps the generation counter and marks the state active. If a tick
// loop is already running or scrolling is not needed, it returns (false, 0).
func (s *SelectionScrollState) NeedsTick() (bool, uint64) {
	if s.ScrollDir != 0 && !s.Active {
		s.Active = true
		s.Gen++
		return true, s.Gen
	}
	return false, 0
}

// HandleTick checks whether an incoming tick with the given generation is
// still valid. Returns true if the tick loop should continue (caller
// should scroll and schedule the next tick). Returns false if the tick
// loop should stop (generation mismatch, direction cleared, or not active).
func (s *SelectionScrollState) HandleTick(gen uint64) bool {
	if s.Gen != gen || s.ScrollDir == 0 || !s.Active {
		s.Active = false
		return false
	}
	return true
}

// Reset clears the scroll state and bumps the generation counter to
// invalidate any in-flight tick. Call on mouse release, selection clear,
// or any event that should stop auto-scrolling.
func (s *SelectionScrollState) Reset() {
	s.ScrollDir = 0
	s.Active = false
	s.Gen++
}
