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
	// ReattachStallTimeout bounds how long a tab or terminal may hold its
	// reattach lock. Both panes release that lock only when the reattach
	// outcome comes back, so an outcome that is dropped, misrouted, or never
	// produced pins the tab in the reattaching state and makes every retry
	// no-op behind the same lock. A reattach is a handful of tmux round-trips
	// against a shared, single-threaded server, so a loaded server can take
	// seconds; this sits well past any real reattach so a sweep only ever
	// releases a lost one, never a slow one.
	ReattachStallTimeout = 45 * time.Second
)
