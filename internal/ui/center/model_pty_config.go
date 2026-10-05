package center

import (
	"time"

	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// Shared PTY tuning constants identical to the sidebar pane live in
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

// PTY constants
const (
	// Diverges from sidebar (50ms): center runs the steady-state flush ceiling
	// tighter so one busy agent tab cannot starve the others' frame cadence.
	ptyFlushMaxInterval = 48 * time.Millisecond
	// Diverges from sidebar (30ms): center's catch-up quiet period is shorter
	// to drain backlogged tabs sooner when the active tab switches.
	ptyFlushQuietAlt = 24 * time.Millisecond
	// Diverges from sidebar (120ms): paired with ptyFlushQuietAlt, center caps
	// catch-up latency lower so a backlogged tab becomes visible faster.
	ptyFlushMaxAlt = 96 * time.Millisecond
	// Inactive tabs still need to advance their terminal state, but can flush less frequently.
	ptyFlushInactiveMultiplier          = 4
	ptyFlushInactiveHeavyMultiplier     = 8
	ptyFlushInactiveVeryHeavyMultiplier = 12
	ptyFlushInactiveMaxIntervalCap      = 250 * time.Millisecond
	ptyHeavyLoadTabThreshold            = 4
	ptyVeryHeavyLoadTabThreshold        = 8
	ptyLoadSampleInterval               = 100 * time.Millisecond
	// Active tab catch-up should drain backlog quickly to avoid visible replay.
	ptyFlushChunkSizeActive = 256 * 1024
	// Catch-up can exceed the steady-state active cap, but it still needs a
	// ceiling so a single actor write cannot monopolize input/scroll handling.
	ptyFlushChunkSizeCatchUp = 1024 * 1024
	// Diverges from sidebar (32): center fans reads across N concurrent agent
	// tabs, so it buffers a deeper read queue per reader.
	ptyReadQueueSize = 64
	// Diverges from sidebar (256K): center allows more in-flight pending bytes
	// because multiple tabs share the backpressure/load-sampling machinery.
	ptyMaxPendingBytes = 512 * 1024
	// Diverges from sidebar (4M): center's larger buffered ceiling absorbs
	// bursts from many tabs before overflow trimming kicks in.
	ptyMaxBufferedBytes  = 8 * 1024 * 1024
	tabActorStallTimeout = 10 * time.Second
	// A background agent that continuously redraws can produce output faster
	// than the intentionally throttled background flush cadence can consume it.
	// Once that deficit is sustained, keeping the live PTY client attached only
	// replays stale frames and grows the buffer. Detach the view before it reaches
	// the overflow ceiling; the tmux session keeps running and selection
	// automatically reattaches to its authoritative current screen.
	ptyBackgroundDetachThreshold = 1024 * 1024
	ptyBackgroundDetachGrace     = 2 * time.Second

	// Backpressure thresholds (inspired by tmux's TTY_BLOCK_START/STOP)
	// When pending output exceeds this, we throttle rendering frequency
	ptyBackpressureMultiplier = 8 // threshold = multiplier * width * height
	ptyBackpressureFlushFloor = 32 * time.Millisecond
)

// PTYOutput is a message containing PTY output data. Gen is the reader
// generation that produced it — output arriving after a restart/reattach
// carries a stale gen and must be dropped rather than appended to the
// replacement stream.
type PTYOutput struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
	Data        []byte
}

// PTYFlush applies buffered PTY output for a tab.
type PTYFlush struct {
	WorkspaceID string
	TabID       TabID
	CatchUp     bool
}

// TabInputFailed reports a terminal input write failure. Gen is the input
// binding generation the failing writer was bound to — a failure arriving
// after the binding was replaced carries a stale gen and must be dropped
// rather than detach the replacement.
type TabInputFailed struct {
	TabID       TabID
	WorkspaceID string
	Err         error
	Gen         uint64
}

// MarkCriticalExternalMsg marks TabInputFailed as critical: dropping it would
// skip the detach/agent cleanup the UI failure path performs.
func (TabInputFailed) MarkCriticalExternalMsg() {}

// TabInputRejected reports a user-input admission rejection (queue
// saturation or a latched binding failure). It carries no payload — Reason is
// a fixed classification, Gen the input binding the verdict applied to — and
// is handled by the app as a warning only: the attachment stays healthy and
// no detach/persist side effect may hang off it.
type TabInputRejected struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
	Reason      string
}

// MarkCriticalExternalMsg marks TabInputRejected as critical so a flood of
// output cannot silently swallow the "your input was dropped" signal.
func (TabInputRejected) MarkCriticalExternalMsg() {}

// PTYCursorRefresh re-renders chat cursor policy when time-based windows expire.
// Gen is the timed-refresh generation (scheduleChatCursorRefreshLocked);
// InputGeneration is the input-binding generation for refreshes emitted by the
// input writer after a delivery — a distinct lifecycle. An InputGeneration
// that no longer matches the tab's current binding is a stale result and the
// refresh is dropped rather than re-arming state on the replacement.
type PTYCursorRefresh struct {
	WorkspaceID     string
	TabID           TabID
	Gen             uint64
	InputGeneration uint64
}

// PTYStopped signals that the PTY read loop has stopped (terminal closed or error).
// Gen is the reader generation that exited — a stopped arriving after a
// restart/reattach carries a stale gen and must be dropped rather than killing
// the replacement reader's cancel channel or burning a restart slot.
type PTYStopped struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
	Err         error
}

// MarkCriticalExternalMsg marks PTYStopped as a critical external message:
// a stopped PTY must never be evicted from the lossy queue.
func (PTYStopped) MarkCriticalExternalMsg() {}

// PTYRestart requests restarting a PTY reader for a tab. Gen is the reader
// generation whose stop is being retried — it must still match the current
// ReaderGen at consume time, or a newer reader already exists and the request
// is moot.
type PTYRestart struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
}

type selectionScrollTick struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
	Seq         uint64
}
