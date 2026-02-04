package center

import "time"

// PTY constants
const (
	ptyFlushQuiet       = 4 * time.Millisecond
	ptyFlushMaxInterval = 16 * time.Millisecond
	ptyFlushQuietAlt    = 8 * time.Millisecond
	ptyFlushMaxAlt      = 32 * time.Millisecond
	// Inactive tabs still need to advance their terminal state, but can flush less frequently.
	ptyFlushInactiveMultiplier = 4
	ptyFlushChunkSize          = 32 * 1024
	ptyReadBufferSize          = 32 * 1024
	ptyReadQueueSize           = 64
	ptyFrameInterval           = time.Second / 60
	ptyMaxPendingBytes         = 512 * 1024
	ptyMaxBufferedBytes        = 8 * 1024 * 1024
	ptyReaderStallTimeout      = 10 * time.Second
	tabActorStallTimeout       = 10 * time.Second
	ptyRestartMax              = 5
	ptyRestartWindow           = time.Minute

	// Backpressure thresholds (inspired by tmux's TTY_BLOCK_START/STOP)
	// When pending output exceeds this, we throttle rendering frequency
	ptyBackpressureMultiplier = 8 // threshold = multiplier * width * height
	ptyBackpressureFlushMin   = 32 * time.Millisecond
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
}
