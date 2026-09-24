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

// SidebarTerminalCreated is a message for terminal creation.
//
// The sidebar's cross-boundary messages conventionally live in
// internal/messages — this family stays leaf-local because its payload
// (*pty.Terminal, ptyio.SessionRestoreCapture) would force
// messages→ptyio→common→messages, an import cycle.
type SidebarTerminalCreated struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
	CaptureCols int
	CaptureRows int
	ptyio.SessionRestoreCapture
}

// MarkCriticalExternalMsg marks SidebarTerminalCreated as critical: the
// message is the only thing that releases the workspace's pendingCreation
// mark — if the lossy queue evicted it, the workspace could never create a
// terminal again for the session's lifetime (plus the built tmux client
// would leak).
func (SidebarTerminalCreated) MarkCriticalExternalMsg() {}

// SidebarTerminalCreateFailed is a message for terminal creation failure
type SidebarTerminalCreateFailed struct {
	WorkspaceID string
	Err         error
}

// MarkCriticalExternalMsg marks SidebarTerminalCreateFailed as critical —
// same pendingCreation release contract as SidebarTerminalCreated.
func (SidebarTerminalCreateFailed) MarkCriticalExternalMsg() {}

type SidebarTerminalReattachResult struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
	CaptureCols int
	CaptureRows int
	ptyio.SessionRestoreCapture
}

// MarkCriticalExternalMsg marks SidebarTerminalReattachResult as critical —
// the reattach path also clears pendingCreation, so a drop wedges it the
// same way a lost create result does.
func (SidebarTerminalReattachResult) MarkCriticalExternalMsg() {}

type SidebarTerminalReattachFailed struct {
	WorkspaceID string
	TabID       TerminalTabID
	Err         error
	Stopped     bool
	Action      string
}

// MarkCriticalExternalMsg marks SidebarTerminalReattachFailed as critical —
// see SidebarTerminalReattachResult.
func (SidebarTerminalReattachFailed) MarkCriticalExternalMsg() {}
