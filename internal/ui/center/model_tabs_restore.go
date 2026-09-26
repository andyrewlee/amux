package center

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func (m *Model) addDetachedTab(ws *data.Workspace, info data.TabInfo) {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	term.AllowAltScreenScrollback = true
	ca := info.CreatedAt
	if ca == 0 {
		ca = time.Now().Unix()
	}
	tab := &Tab{
		ID:            generateTabID(),
		Name:          displayName,
		Assistant:     info.Assistant,
		Workspace:     ws,
		SessionName:   info.SessionName,
		Detached:      true,
		Running:       false,
		Terminal:      term,
		createdAt:     ca,
		lastFocusedAt: time.Unix(ca, 0),
	}
	isChat := m.isChatTab(tab)
	term.IgnoreCursorVisibilityControls = false
	term.TreatLFAsCRLF = isChat
	term.CaptureNormalScreenOnClear = isChat
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
	m.markHelpDirty()
}

// addPlaceholderTab synchronously creates a placeholder tab in the correct slice
// position. The tab starts detached and non-running; an async reattach upgrades
// it in-place (by TabID) without changing slice order.
func (m *Model) addPlaceholderTab(ws *data.Workspace, info data.TabInfo) (TabID, string, uint64) {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	term.AllowAltScreenScrollback = true
	tabID := generateTabID()
	sessionName := strings.TrimSpace(info.SessionName)
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	ca := info.CreatedAt
	if ca == 0 {
		ca = time.Now().Unix()
	}
	tab := &Tab{
		ID:            tabID,
		Name:          displayName,
		Assistant:     info.Assistant,
		Workspace:     ws,
		SessionName:   sessionName,
		Detached:      true,
		Running:       false,
		Terminal:      term,
		createdAt:     ca,
		lastFocusedAt: time.Unix(ca, 0),
	}
	// Placeholder tabs are immediately queued for async reattach, so they are
	// born holding the reattach lock. Take it the same way every other path
	// does, so it is stamped and versioned rather than a bare flag.
	_ = tab.beginReattachLocked()
	isChat := m.isChatTab(tab)
	term.IgnoreCursorVisibilityControls = false
	term.TreatLFAsCRLF = isChat
	term.CaptureNormalScreenOnClear = isChat
	wsID := string(ws.ID())
	m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
	m.markHelpDirty()
	return tabID, sessionName, tab.reattachEpochLocked()
}

// reattachToSession returns a tea.Cmd that asynchronously connects a placeholder
// tab to its tmux session. On success it produces ptyTabReattachResult which
// updates the tab in-place (by TabID). On failure it produces ptyTabReattachFailed.
func (m *Model) reattachToSession(ws *data.Workspace, tabID TabID, assistant, sessionName string, epoch uint64) tea.Cmd {
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	tm := m.terminalMetrics()
	attachWidth := tm.Width
	attachHeight := tm.Height
	opts := m.tmuxOpts
	assistantCfg, cfgOK := assistantConfigSnapshot(m.config, assistant)
	if !cfgOK {
		err := fmt.Errorf("unknown agent type: %s", assistant)
		return func() tea.Msg {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Action:      "reattach",
			}
		}
	}
	return func() tea.Msg {
		state, err := sessionStateForFn(sessionName, opts)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Action:      "reattach",
			}
		}
		if !state.Exists || !state.HasLivePane {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         errors.New("tmux session ended"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		// Ownership check shared with the other attach paths: a live session
		// under the stored name must carry our tags, else it is a foreign
		// squatter and reattach refuses rather than attaching to it.
		owned, ownErr := sessionOwnedFn(sessionName, data.WorkspaceIdentityStrings(ws), opts)
		if ownErr != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         ownErr,
				Action:      "reattach",
			}
		}
		if !owned {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         errors.New("tmux session is not owned by this workspace"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		tags := ptyio.AttachSessionTags(ws, string(tabID), "agent", assistant, m.instanceID, false)
		bootstrap := ptyio.DefaultBootstrap().CaptureExisting(sessionName, termWidth, termHeight, opts)
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(attachHeight, attachWidth)
		agent, err := createAgentWithConfigFn(
			m.agentManager,
			ws,
			appPty.AgentType(assistant),
			sessionName,
			ptyRows,
			ptyCols,
			tags,
			assistantCfg,
		)
		if err != nil {
			ptyio.DefaultBootstrap().Rollback(sessionName, bootstrap, opts)
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Action:      "reattach",
			}
		}
		scrollback, postAttachScrollback, captureFullPane, snapshot, captureCols, captureRows := ptyio.FinalizeAttachScrollback(sessionName, bootstrap, attachWidth, attachHeight, opts, capturePaneFn)
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Epoch:       epoch,
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
}
