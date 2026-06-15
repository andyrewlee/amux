package ptyio

import "time"

// Shared PTY flush/buffer tuning constants used by both the center (agent
// tabs) and sidebar (single terminal) panes. Only values that are genuinely
// identical in both panes live here; pane-specific overrides (e.g. center's
// concurrent-tab backpressure or sidebar's single-terminal buffer sizing) stay
// local to their config files and are documented there with a one-line reason.
const (
	// PtyFlushQuiet is the quiet period output must be idle for before a steady
	// flush fires.
	PtyFlushQuiet = 12 * time.Millisecond
	// PtyFlushChunkSize bounds the bytes drained per steady-state flush.
	PtyFlushChunkSize = 32 * 1024
	// PtyReadBufferSize is the size of the PTY reader's read buffer.
	PtyReadBufferSize = 32 * 1024
	// PtyFrameInterval is the render cadence (24 fps) for PTY output.
	PtyFrameInterval = time.Second / 24
	// PtyReaderStallTimeout is how long a reader may go silent before it is
	// treated as stalled.
	PtyReaderStallTimeout = 10 * time.Second
	// PtyRestartMax is the max reader restarts allowed within PtyRestartWindow.
	PtyRestartMax = 5
	// PtyRestartWindow is the sliding window for counting reader restarts.
	PtyRestartWindow = time.Minute
)
