package vterm

import "github.com/andyrewlee/amux/internal/logging"

// altScreenCaptureState tracks the alt-screen frame currently reserved in
// scrollback. captureScreenToScrollback copies the visible alt-screen frame
// into scrollback before each erase-display redraw; this state remembers
// where that frame sits so the next redraw can dedup, replace, or transition
// it instead of appending duplicates.
//
// Invariants (checked by users of this struct):
//   - tracked implies 0 < dropLen <= frameLen
//   - frameLen + endOffset <= len(Scrollback)
//   - endOffset > 0 only while tracked
type altScreenCaptureState struct {
	// frameLen is the full reserved frame length in scrollback.
	frameLen int
	// dropLen is the removable suffix length of the reserved frame (rows the
	// capture added itself, as opposed to rows that already overlapped).
	dropLen int
	// tracked marks whether the frame's position is still known after
	// scrollUp appended rows behind it.
	tracked bool
	// endOffset counts scrollUp rows that accumulated after the tracked frame.
	endOffset int
}

// reset clears the tracking state (normal completion or invalidation).
func (c *altScreenCaptureState) reset() {
	*c = altScreenCaptureState{}
}

// resetInvalid clears the tracking state after an invariant violation,
// logging the observed state so violations are diagnosable instead of being
// silently swallowed.
func (c *altScreenCaptureState) resetInvalid(reason string) {
	logging.Warn("alt-screen capture: invariant violation (%s): frameLen=%d dropLen=%d tracked=%v endOffset=%d",
		reason, c.frameLen, c.dropLen, c.tracked, c.endOffset)
	c.reset()
}
