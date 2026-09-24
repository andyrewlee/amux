package ptyio

import "github.com/andyrewlee/amux/internal/tmux"

// Test seams for the session-bootstrap flow, shared by every pane that
// reattaches (center agent tabs and the sidebar terminal). Tests overriding
// these must not use t.Parallel in their package.
var (
	ProbeSessionFn           = tmux.ProbeSession
	ResizePaneToSizeFn       = tmux.ResizePaneToSize
	CapturePaneFullDataFn    = tmux.CapturePaneFullData
	CapturePaneHistoryDataFn = tmux.CapturePaneHistoryData
)

// DefaultBootstrap builds a SessionBootstrap from the package's seam vars.
// It is rebuilt per call (reading the vars each time) so a test that overrides
// a seam var still flows through the next bootstrap operation.
func DefaultBootstrap() SessionBootstrap {
	return SessionBootstrap{
		Fns: SessionBootstrapFns{
			ProbeSession:           ProbeSessionFn,
			ResizePaneToSize:       ResizePaneToSizeFn,
			CapturePaneFullData:    CapturePaneFullDataFn,
			CapturePaneHistoryData: CapturePaneHistoryDataFn,
		},
	}
}
