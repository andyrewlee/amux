package sidebar

import (
	"time"

	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// Shared PTY tuning constants identical to the center pane live in
// internal/ui/ptyio (ptyio.PtyFlushQuiet etc.); they are aliased here so the
// call sites keep their short package-local names.
const (
	ptyFlushQuiet         = ptyio.PtyFlushQuiet
	ptyFlushChunkSize     = ptyio.PtyFlushChunkSize
	ptyReadBufferSize     = ptyio.PtyReadBufferSize
	ptyFrameInterval      = ptyio.PtyFrameInterval
	ptyReaderStallTimeout = ptyio.PtyReaderStallTimeout
	ptyRestartMax         = ptyio.PtyRestartMax
	ptyRestartWindow      = ptyio.PtyRestartWindow
)

const (
	// Diverges from center (48ms): the sidebar drives a single terminal, so it
	// can afford a slightly looser flush ceiling than center's N-tab cadence.
	ptyFlushMaxInterval = 50 * time.Millisecond
	// Diverges from center (24ms): single-terminal catch-up has no competing
	// tabs, so the quiet period can be longer to coalesce more output.
	ptyFlushQuietAlt = 30 * time.Millisecond
	// Diverges from center (96ms): paired with ptyFlushQuietAlt, the sidebar
	// tolerates higher catch-up latency for a single terminal.
	ptyFlushMaxAlt = 120 * time.Millisecond
	// Diverges from center (64): a single terminal needs a shallower read queue.
	ptyReadQueueSize = 32
	// Diverges from center (512K): one terminal needs fewer in-flight pending
	// bytes than center's shared multi-tab backpressure budget.
	ptyMaxPendingBytes = 256 * 1024
	// Diverges from center (8M): a single terminal needs a smaller buffered
	// ceiling before overflow trimming.
	ptyMaxBufferedBytes = 4 * 1024 * 1024
)

// SidebarTerminalCreated is a message for terminal creation
type SidebarTerminalCreated struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
	CaptureCols int
	CaptureRows int
	ptyio.SessionRestoreCapture
}

// SidebarTerminalCreateFailed is a message for terminal creation failure
type SidebarTerminalCreateFailed struct {
	WorkspaceID string
	Err         error
}

type SidebarTerminalReattachResult struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
	CaptureCols int
	CaptureRows int
	ptyio.SessionRestoreCapture
}

type SidebarTerminalReattachFailed struct {
	WorkspaceID string
	TabID       TerminalTabID
	Err         error
	Stopped     bool
	Action      string
}
