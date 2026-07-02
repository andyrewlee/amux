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

	// Backpressure thresholds (inspired by tmux's TTY_BLOCK_START/STOP)
	// When pending output exceeds this, we throttle rendering frequency
	ptyBackpressureMultiplier = 8 // threshold = multiplier * width * height
	ptyBackpressureFlushFloor = 32 * time.Millisecond
)

// PTYOutput is a message containing PTY output data
type PTYOutput struct {
	WorkspaceID string
	TabID       TabID
	Data        []byte
}

// PTYTick triggers a PTY read
type PTYTick struct {
	WorkspaceID string
	TabID       TabID
}

// PTYFlush applies buffered PTY output for a tab.
type PTYFlush struct {
	WorkspaceID string
	TabID       TabID
	CatchUp     bool
}

// PTYCursorRefresh re-renders chat cursor policy when time-based windows expire.
type PTYCursorRefresh struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
}

// PTYStopped signals that the PTY read loop has stopped (terminal closed or error)
type PTYStopped struct {
	WorkspaceID string
	TabID       TabID
	Err         error
}

// PTYRestart requests restarting a PTY reader for a tab.
type PTYRestart struct {
	WorkspaceID string
	TabID       TabID
}

type selectionScrollTick struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
	Seq         uint64
}
