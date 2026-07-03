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
	// TickSeq is the next tick sequence expected for Gen. Duplicate requests for
	// the same sequence are harmless: the first tick advances TickSeq and later
	// copies are ignored without stopping the loop.
	TickSeq uint64
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

// NeedsTick reports whether the caller should schedule an auto-scroll tick and,
// if so, returns the current generation and next expected tick sequence.
// Motions while a loop is already active re-request the same generation/sequence
// instead of invalidating the live timer. If a request was dropped, the next
// motion can replace it; if both copies arrive, HandleTick accepts whichever
// one arrives first and rejects the duplicate by sequence number.
func (s *SelectionScrollState) NeedsTick() (bool, uint64, uint64) {
	if s.ScrollDir == 0 {
		return false, 0, 0
	}
	if !s.Active || s.TickSeq == 0 {
		s.Active = true
		s.Gen++
		s.TickSeq = 1
	}
	return true, s.Gen, s.TickSeq
}

// HandleTick checks whether an incoming tick with the given generation is
// still valid. Returns true if the tick loop should continue (caller should
// scroll and schedule the next tick). Stale generations or duplicate sequences
// are ignored without stopping the current generation; explicit stop conditions
// (direction cleared or inactive state) end the loop.
func (s *SelectionScrollState) HandleTick(gen, seq uint64) bool {
	if !s.Active {
		return false
	}
	if s.ScrollDir == 0 {
		s.Active = false
		return false
	}
	if s.Gen != gen || s.TickSeq != seq {
		return false
	}
	s.TickSeq++
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
