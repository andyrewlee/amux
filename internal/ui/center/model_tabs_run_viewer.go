package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// createRunAttachFn is the test seam for run-session attach; see the sibling
// seams in model_tabs_session_reattach.go for the no-parallel contract.
var createRunAttachFn = func(
	manager *appPty.AgentManager,
	ws *data.Workspace,
	sessionName string,
	rows, cols uint16,
) (*appPty.Agent, error) {
	return manager.CreateRunAttach(ws, sessionName, rows, cols)
}

// CreateRunViewerTab opens an interactive center tab attached to an existing
// run session (`@amux_type=run`). Unlike the agent/viewer paths it attaches
// a session the tab does NOT own: no tags are stamped, no session options are
// applied, and closing the tab detaches instead of killing (Tab.DetachOnly)
// so the script outlives the viewer.
func (m *Model) CreateRunViewerTab(ws *data.Workspace, sessionName string) tea.Cmd {
	if ws == nil || sessionName == "" {
		return nil
	}
	tm := m.terminalMetrics()
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	ptyRows, ptyCols, _ := appPty.WinsizeFromInts(tm.Height, tm.Width)
	tabID := generateTabID()
	opts := m.tmuxOpts

	return func() tea.Msg {
		// The run session can exit between the app-layer lookup and this
		// attach — re-check attachable rather than trusting the caller.
		state, err := sessionStateForFn(sessionName, opts)
		if err != nil {
			return messages.Error{Err: err, Context: "run session lookup"}
		}
		if !ptyio.SessionAttachable(state) {
			return messages.Toast{
				Message: "Run session ended before attach",
				Level:   messages.ToastInfo,
			}
		}
		bootstrap := ptyio.DefaultBootstrap().CaptureExisting(sessionName, termWidth, termHeight, opts)
		agent, err := createRunAttachFn(m.agentManager, ws, sessionName, ptyRows, ptyCols)
		if err != nil {
			ptyio.DefaultBootstrap().Rollback(sessionName, bootstrap, opts)
			return messages.Error{Err: err, Context: "run session attach"}
		}
		scrollback, postAttachScrollback, captureFullPane, snapshot, captureCols, captureRows := ptyio.FinalizeAttachScrollback(sessionName, bootstrap, tm.Width, tm.Height, opts, capturePaneFn)
		return ptyTabCreateResult{
			Workspace:  ws,
			Assistant:  "run",
			Agent:      agent,
			TabID:      tabID,
			Activate:   true,
			DetachOnly: true,
			Rows:       captureRows,
			Cols:       captureCols,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture:           scrollback,
				PostAttachScrollbackCapture: postAttachScrollback,
				CaptureFullPane:             captureFullPane,
				SnapshotCols:                snapshot.Cols,
				SnapshotRows:                snapshot.Rows,
				SnapshotCursorX:             snapshot.CursorX,
				SnapshotCursorY:             snapshot.CursorY,
				SnapshotHasCursor:           snapshot.HasCursor,
				SnapshotModeState:           snapshot.ModeState,
			},
		}
	}
}
