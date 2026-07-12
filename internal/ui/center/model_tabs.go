package center

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/clipperhouse/displaywidth"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func nextAssistantName(assistant string, tabs []*Tab) string {
	assistant = strings.TrimSpace(assistant)
	if assistant == "" {
		return ""
	}

	used := make(map[string]struct{})
	for _, tab := range tabs {
		if tab == nil || tab.Assistant != assistant {
			continue
		}
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = assistant
		}
		used[name] = struct{}{}
	}

	if _, ok := used[assistant]; !ok {
		return assistant
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", assistant, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

type ptyTabCreateResult struct {
	Workspace                   *data.Workspace
	Assistant                   string
	DisplayName                 string
	Agent                       *appPty.Agent
	TabID                       TabID
	Activate                    bool
	Rows                        int
	Cols                        int
	ScrollbackCapture           []byte
	PostAttachScrollbackCapture []byte
	CaptureFullPane             bool
	SnapshotCols                int
	SnapshotRows                int
	SnapshotCursorX             int
	SnapshotCursorY             int
	SnapshotHasCursor           bool
	SnapshotModeState           tmux.PaneModeState
}

type ptyTabReattachResult struct {
	WorkspaceID                 string
	TabID                       TabID
	Agent                       *appPty.Agent
	Rows                        int
	Cols                        int
	ScrollbackCapture           []byte
	PostAttachScrollbackCapture []byte
	CaptureFullPane             bool
	SnapshotCols                int
	SnapshotRows                int
	SnapshotCursorX             int
	SnapshotCursorY             int
	SnapshotHasCursor           bool
	SnapshotModeState           tmux.PaneModeState
}

type ptyTabReattachFailed struct {
	WorkspaceID string
	TabID       TabID
	Err         error
	Stopped     bool
	Action      string
}

func truncateDisplayName(name string) string {
	const (
		maxWidth     = 20
		prefix       = "..."
		suffixBudget = maxWidth - len(prefix)
	)
	if displaywidth.String(name) <= maxWidth {
		return name
	}

	clusters := make([]string, 0, len(name))
	widths := make([]int, 0, len(name))
	graphemes := displaywidth.StringGraphemes(name)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Value())
		widths = append(widths, graphemes.Width())
	}

	width := 0
	start := len(clusters)
	for i := len(clusters) - 1; i >= 0; i-- {
		if width+widths[i] > suffixBudget {
			break
		}
		width += widths[i]
		start = i
	}
	return prefix + strings.Join(clusters[start:], "")
}

// createAgentTab creates a new agent tab
func (m *Model) createAgentTab(assistant string, ws *data.Workspace) tea.Cmd {
	return m.createAgentTabWithSession(assistant, ws, "", "", true)
}

func (m *Model) createAgentTabWithSession(assistant string, ws *data.Workspace, sessionName, displayName string, activate bool) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("no workspace selected"), Context: "creating agent"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	tabID := generateTabID()
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}

	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s workspace=%s", assistant, ws.Name)
		now := time.Now()

		tags := tmux.SessionTags{
			WorkspaceID:  string(ws.ID()),
			TabID:        string(tabID),
			Type:         "agent",
			Assistant:    assistant,
			CreatedAt:    now.Unix(),
			InstanceID:   m.instanceID,
			SessionOwner: m.instanceID,
			LeaseAtMS:    now.UnixMilli(),
		}
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(termHeight, termWidth)
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, ptyRows, ptyCols, tags)
		if err != nil {
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		// Fresh tabs must only seed history. The attached PTY still has unread
		// startup bytes queued, so preloading the visible screen would replay the
		// same banner/prompt a second time when the reader drains.
		captureCols, captureRows := sessionHistoryCaptureSize(sessionName, termWidth, termHeight, m.tmuxOpts)
		scrollback, _ := tmux.CapturePane(sessionName, m.tmuxOpts)

		return ptyTabCreateResult{
			Workspace:         ws,
			Assistant:         assistant,
			Agent:             agent,
			TabID:             tabID,
			DisplayName:       displayName,
			Activate:          activate,
			Rows:              captureRows,
			Cols:              captureCols,
			ScrollbackCapture: scrollback,
			CaptureFullPane:   false,
			SnapshotCols:      termWidth,
			SnapshotRows:      termHeight,
		}
	}
}

func (m *Model) handlePtyTabCreated(msg ptyTabCreateResult) tea.Cmd {
	if msg.Workspace == nil || msg.Agent == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("missing workspace or agent"), Context: "creating terminal tab"}
		}
	}
	if msg.TabID == "" {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("missing tab id"), Context: "creating terminal tab"}
		}
	}
	now := time.Now()

	captureRows := msg.Rows
	captureCols := msg.Cols
	cols, rows := m.sessionRestoreLiveSize(msg.CaptureFullPane, captureCols, captureRows)
	initialCols, initialRows := ptyio.SessionSnapshotSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows, cols, rows)

	wsID := string(msg.Workspace.ID())
	tabs := m.tabs.ByWorkspace[wsID]
	var existing *Tab
	existingIdx := -1
	if msg.TabID != "" {
		for i, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			if tab.ID == msg.TabID {
				existing = tab
				existingIdx = i
				break
			}
		}
	}

	displayName := strings.TrimSpace(msg.DisplayName)

	if existing != nil {
		if displayName == "" {
			displayName = strings.TrimSpace(msg.Assistant)
			if displayName == "" {
				displayName = "Terminal"
			}
		}
		tabID := existing.ID
		tab := existing
		m.stopPTYReader(tab)
		tab.mu.Lock()
		oldAgent := tab.Agent
		createdTerminal := false
		if tab.Terminal == nil {
			tab.Terminal = vterm.New(initialCols, initialRows)
			createdTerminal = true
		}
		tab.Assistant = msg.Assistant
		if tab.Terminal != nil {
			// Do not reset parser state when reusing an existing terminal here.
			// pendingOutput may still contain continuation bytes queued under the
			// current parser carry, and recreate must preserve that continuity until
			// buffered output is explicitly reconciled.
			tab.Terminal.AllowAltScreenScrollback = true
			m.applyTerminalCursorPolicyLocked(tab)
			if msg.CaptureFullPane {
				// A full tmux pane snapshot supersedes any preserved local PTY
				// backlog for this terminal state.
				tab.PendingOutput = nil
				ptyio.RestorePaneCapture(
					tab.Terminal,
					msg.ScrollbackCapture,
					msg.PostAttachScrollbackCapture,
					msg.SnapshotCursorX,
					msg.SnapshotCursorY,
					msg.SnapshotHasCursor,
					msg.SnapshotModeState,
					msg.SnapshotCols,
					msg.SnapshotRows,
					cols,
					rows,
				)
			} else if createdTerminal || len(tab.Terminal.Scrollback) == 0 {
				ptyio.RestoreScrollbackCapture(tab.Terminal, msg.ScrollbackCapture, captureCols, captureRows, cols, rows)
			} else if m.width > 0 && m.height > 0 {
				ptyio.ResizeTerminalForSessionRestore(tab.Terminal, cols, rows)
			}
		}
		if tab.Name == "" {
			tab.Name = displayName
		}
		tab.Workspace = msg.Workspace
		tab.Agent = msg.Agent
		tab.SessionName = msg.Agent.Session
		tab.markAttachedLocked()
		tab.resetActorWriteStateLocked()
		m.applyTerminalCursorPolicyLocked(tab)
		if tab.createdAt == 0 {
			tab.createdAt = now.Unix()
		}
		if tab.lastFocusedAt.IsZero() {
			tab.lastFocusedAt = now
		}
		resetChatCursorActivityStateLocked(tab)
		tab.mu.Unlock()
		tab.resetActivityANSIState()
		if oldAgent != nil && oldAgent != msg.Agent {
			_ = m.agentManager.CloseAgent(oldAgent)
		}

		// Set up response writer for terminal queries (DSR, DA, etc.)
		if msg.Agent.Terminal != nil && tab.Terminal != nil {
			agentTerm := msg.Agent.Terminal
			workspaceID := wsID
			tab.Terminal.SetResponseWriter(func(data []byte) {
				if len(data) == 0 || agentTerm == nil {
					return
				}
				if err := agentTerm.SendString(string(data)); err != nil {
					logging.Warn("Response write failed for tab %s: %v", tabID, err)
					if m.msgSink != nil {
						m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
					}
				}
			})
		}

		// Set PTY size to match
		if msg.Agent.Terminal != nil {
			m.resizePTY(tab, rows, cols)
		}
		_ = m.startPTYReader(wsID, tab)

		if msg.Activate && existingIdx >= 0 {
			m.setActiveTabIdxForWorkspace(wsID, existingIdx)
		}
		m.noteTabsChanged()

		return func() tea.Msg {
			return messages.TabCreated{Index: existingIdx, Name: tab.Name}
		}
	}

	if displayName == "" {
		displayName = nextAssistantName(msg.Assistant, tabs)
	}
	if displayName == "" {
		displayName = "Terminal"
	}

	// Create virtual terminal emulator with scrollback
	term := vterm.New(initialCols, initialRows)
	term.AllowAltScreenScrollback = true

	// Create tab with the caller-provided stable ID so tmux/session reconciliation
	// cannot silently drift onto a different tab.
	tabID := msg.TabID
	tab := &Tab{
		ID:            tabID,
		Name:          displayName,
		Assistant:     msg.Assistant,
		Workspace:     msg.Workspace,
		Agent:         msg.Agent,
		SessionName:   msg.Agent.Session,
		Terminal:      term,
		Running:       true, // Agent/viewer starts running
		createdAt:     now.Unix(),
		lastFocusedAt: now,
	}
	isChat := m.isChatTab(tab)
	term.IgnoreCursorVisibilityControls = false
	term.TreatLFAsCRLF = isChat
	term.CaptureNormalScreenOnClear = isChat
	if msg.CaptureFullPane {
		ptyio.RestorePaneCapture(
			term,
			msg.ScrollbackCapture,
			msg.PostAttachScrollbackCapture,
			msg.SnapshotCursorX,
			msg.SnapshotCursorY,
			msg.SnapshotHasCursor,
			msg.SnapshotModeState,
			msg.SnapshotCols,
			msg.SnapshotRows,
			cols,
			rows,
		)
	} else {
		ptyio.RestoreScrollbackCapture(term, msg.ScrollbackCapture, captureCols, captureRows, cols, rows)
	}

	// Set up response writer for terminal queries (DSR, DA, etc.)
	if msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := string(msg.Workspace.ID())
		term.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if err := agentTerm.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				if m.msgSink != nil {
					m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
				}
			}
		})
	}

	// Set PTY size to match
	if msg.Agent.Terminal != nil {
		m.resizePTY(tab, rows, cols)
	}

	// Add tab to the workspace's tab list
	m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
	createdIdx := len(m.tabs.ByWorkspace[wsID]) - 1
	if msg.Activate {
		m.setActiveTabIdxForWorkspace(wsID, createdIdx)
	}
	m.noteTabsChanged()

	return func() tea.Msg {
		return messages.TabCreated{Index: createdIdx, Name: displayName}
	}
}
