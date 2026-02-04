package sidebar

import (
	"time"

	"github.com/andyrewlee/amux/internal/pty"
)

const (
	ptyFlushQuiet         = 12 * time.Millisecond
	ptyFlushMaxInterval   = 50 * time.Millisecond
	ptyFlushQuietAlt      = 30 * time.Millisecond
	ptyFlushMaxAlt        = 120 * time.Millisecond
	ptyFlushChunkSize     = 32 * 1024
	ptyReadBufferSize     = 32 * 1024
	ptyReadQueueSize      = 32
	ptyFrameInterval      = time.Second / 60
	ptyMaxPendingBytes    = 256 * 1024
	ptyReaderStallTimeout = 10 * time.Second
	ptyMaxBufferedBytes   = 4 * 1024 * 1024
	ptyRestartMax         = 5
	ptyRestartWindow      = time.Minute
)

// SidebarTerminalCreated is a message for terminal creation
type SidebarTerminalCreated struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
	Scrollback  []byte
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
	Scrollback  []byte
}

type SidebarTerminalReattachFailed struct {
	WorkspaceID string
	TabID       TerminalTabID
	Err         error
	Stopped     bool
	Action      string
}
