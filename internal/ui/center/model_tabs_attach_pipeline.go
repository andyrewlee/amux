package center

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// attachPipelineParams carries every input the shared attach pipeline needs;
// both callers (interactive ReattachActiveTab, startup reattachToSession)
// fill the same struct. termWidth/termHeight are the bootstrap capture dims
// (the session's own viewport), attachWidth/attachHeight the terminal metrics
// dims used for winsize and scrollback finalization.
type attachPipelineParams struct {
	ws           *data.Workspace
	tabID        TabID
	assistant    string
	assistantCfg config.AssistantConfig
	sessionName  string
	epoch        uint64
	termWidth    int
	termHeight   int
	attachWidth  int
	attachHeight int
}

// attachSessionPipeline is the single attach sequence both center attach
// paths run — it used to live as two ~90-line near-identical closures that
// drifted (restore inlined the dead-session check while reattach called the
// policy home). Steps: session state → SessionAttachable → SessionOwned →
// attach tags → bootstrap capture → winsize → agent create (rollback on
// failure) → FinalizeAttachScrollback → result.
//
// ptyio.SessionAttachable and ptyio.SessionOwned are THE dead-session and
// ownership policies — a semantics change lands in ptyio or nowhere, never
// re-inlined here. The helper lives in center (not ptyio) because its
// results are center message types.
func (m *Model) attachSessionPipeline(p attachPipelineParams) tea.Msg {
	opts := m.tmuxOpts
	fail := func(err error, stopped bool) tea.Msg {
		return ptyTabReattachFailed{
			WorkspaceID: string(p.ws.ID()),
			TabID:       p.tabID,
			Epoch:       p.epoch,
			Err:         err,
			Stopped:     stopped,
			Action:      "reattach",
		}
	}
	state, err := sessionStateForFn(p.sessionName, opts)
	if err != nil {
		return fail(err, false)
	}
	// Dead-session policy is ptyio.SessionAttachable, shared with sidebar:
	// reattach only attaches to a live session — a missing or dead session
	// reports Stopped so the user chooses restart explicitly.
	if !ptyio.SessionAttachable(state) {
		return fail(errors.New("tmux session ended"), true)
	}
	// Ownership policy is ptyio.SessionOwned, shared with sidebar: the live
	// session must carry our tags. A foreign session squatting the name
	// reports Stopped so the user restarts explicitly — restart kills the
	// squatter by name before recreating.
	owned, ownErr := sessionOwnedFn(p.sessionName, data.WorkspaceIdentityStrings(p.ws), opts)
	if ownErr != nil {
		return fail(ownErr, false)
	}
	if !owned {
		return fail(errors.New("tmux session is not owned by this workspace"), true)
	}
	tags := ptyio.AttachSessionTags(p.ws, string(p.tabID), "agent", p.assistant, m.instanceID, false)
	bootstrap := ptyio.DefaultBootstrap().CaptureExisting(p.sessionName, p.termWidth, p.termHeight, opts)
	ptyRows, ptyCols, _ := appPty.WinsizeFromInts(p.attachHeight, p.attachWidth)
	agent, err := createAgentWithConfigFn(
		m.agentManager,
		p.ws,
		appPty.AgentType(p.assistant),
		p.sessionName,
		ptyRows,
		ptyCols,
		tags,
		p.assistantCfg,
	)
	if err != nil {
		ptyio.DefaultBootstrap().Rollback(p.sessionName, bootstrap, opts)
		return fail(err, false)
	}
	scrollback, postAttachScrollback, captureFullPane, snapshot, captureCols, captureRows := ptyio.FinalizeAttachScrollback(p.sessionName, bootstrap, p.attachWidth, p.attachHeight, opts, capturePaneFn)
	return ptyTabReattachResult{
		WorkspaceID: string(p.ws.ID()),
		TabID:       p.tabID,
		Epoch:       p.epoch,
		Agent:       agent,
		Rows:        captureRows,
		Cols:        captureCols,
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
