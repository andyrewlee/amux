package sidebar

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// flushTiming returns the appropriate flush timing
func (m *TerminalModel) flushTiming() (time.Duration, time.Duration) {
	ts := m.getTerminal()
	if ts == nil {
		return ptyFlushQuiet, ptyFlushMaxInterval
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if ts.VTerm != nil && ts.VTerm.AltScreen {
		return ptyFlushQuietAlt, ptyFlushMaxAlt
	}
	return ptyFlushQuiet, ptyFlushMaxInterval
}

// Init initializes the terminal model
func (m *TerminalModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *TerminalModel) Update(msg tea.Msg) (*TerminalModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		ts := m.getTerminal()
		if ts == nil || ts.VTerm == nil {
			return m, nil
		}
		ts.mu.Lock()
		delta := common.ScrollDeltaForHeight(ts.VTerm.Height, 8) // ~12.5% of viewport
		if msg.Button == tea.MouseWheelUp {
			ts.VTerm.ScrollView(delta)
		} else if msg.Button == tea.MouseWheelDown {
			ts.VTerm.ScrollView(-delta)
		}
		ts.mu.Unlock()
		return m, nil

	case tea.PasteMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Handle bracketed paste - send entire content at once with escape sequences
		text := msg.Content
		bracketedText := "\x1b[200~" + text + "\x1b[201~"
		if err := ts.Terminal.SendString(bracketedText); err != nil {
			logging.Warn("Sidebar paste failed: %v", err)
			m.detachState(ts)
		}
		logging.Debug("Sidebar terminal pasted %d bytes via bracketed paste", len(text))
		return m, nil

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Check if this is Cmd+C (copy command)
		k := msg.Key()
		isCopyKey := k.Mod.Contains(tea.ModSuper) && k.Code == 'c'

		// Handle explicit Cmd+C to copy current selection
		if isCopyKey {
			ts.mu.Lock()
			if ts.VTerm != nil && ts.VTerm.HasSelection() {
				text := ts.VTerm.GetSelectedText(
					ts.VTerm.SelStartX(), ts.VTerm.SelStartLine(),
					ts.VTerm.SelEndX(), ts.VTerm.SelEndLine(),
				)
				if text != "" {
					if err := common.CopyToClipboard(text); err != nil {
						logging.Error("Failed to copy to clipboard: %v", err)
					} else {
						logging.Info("Cmd+C copied %d chars from sidebar", len(text))
					}
				}
			}
			ts.mu.Unlock()
			return m, nil // Don't forward to terminal, don't clear selection
		}

		// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
		switch msg.Key().Code {
		case tea.KeyPgUp:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case tea.KeyPgDown:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(-ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil
		}

		// If scrolled, any typing goes back to live and sends key
		ts.mu.Lock()
		if ts.VTerm != nil && ts.VTerm.IsScrolled() {
			ts.VTerm.ScrollViewToBottom()
		}
		ts.mu.Unlock()

		// Forward ALL keys to terminal (no Ctrl interceptions)
		input := common.KeyToBytes(msg)
		if len(input) > 0 {
			if err := ts.Terminal.SendString(string(input)); err != nil {
				logging.Warn("Sidebar input failed: %v", err)
				m.detachState(ts)
			}
		}

	case messages.SidebarPTYOutput:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			ts.pendingOutput = append(ts.pendingOutput, msg.Data...)
			if len(ts.pendingOutput) > ptyMaxBufferedBytes {
				overflow := len(ts.pendingOutput) - ptyMaxBufferedBytes
				perf.Count("sidebar_pty_drop_bytes", int64(overflow))
				ts.pendingOutput = append([]byte(nil), ts.pendingOutput[overflow:]...)
			}
			ts.lastOutputAt = time.Now()
			if !ts.flushScheduled {
				ts.flushScheduled = true
				ts.flushPendingSince = ts.lastOutputAt
				quiet, _ := m.flushTiming()
				cmds = append(cmds, common.SafeTick(quiet, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
				}))
			}
		}

	case messages.SidebarPTYFlush:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			now := time.Now()
			quietFor := now.Sub(ts.lastOutputAt)
			pendingFor := time.Duration(0)
			if !ts.flushPendingSince.IsZero() {
				pendingFor = now.Sub(ts.flushPendingSince)
			}
			quiet, maxInterval := m.flushTiming()
			if quietFor < quiet && pendingFor < maxInterval {
				delay := quiet - quietFor
				if delay < time.Millisecond {
					delay = time.Millisecond
				}
				ts.flushScheduled = true
				cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
				}))
				break
			}

			ts.flushScheduled = false
			ts.flushPendingSince = time.Time{}
			if len(ts.pendingOutput) > 0 {
				ts.mu.Lock()
				if ts.VTerm != nil {
					chunkSize := len(ts.pendingOutput)
					if chunkSize > ptyFlushChunkSize {
						chunkSize = ptyFlushChunkSize
					}
					ts.VTerm.Write(ts.pendingOutput[:chunkSize])
					copy(ts.pendingOutput, ts.pendingOutput[chunkSize:])
					ts.pendingOutput = ts.pendingOutput[:len(ts.pendingOutput)-chunkSize]
				}
				ts.mu.Unlock()
				if len(ts.pendingOutput) == 0 {
					ts.pendingOutput = ts.pendingOutput[:0]
				} else {
					ts.flushScheduled = true
					ts.flushPendingSince = time.Now()
					cmds = append(cmds, common.SafeTick(time.Millisecond, func(t time.Time) tea.Msg {
						return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
					}))
				}
			}
		}

	case messages.SidebarPTYStopped:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			termAlive := ts.Terminal != nil && !ts.Terminal.IsClosed()
			m.stopPTYReader(ts)
			if termAlive {
				shouldRestart := true
				var backoff time.Duration
				ts.mu.Lock()
				if ts.ptyRestartSince.IsZero() || time.Since(ts.ptyRestartSince) > ptyRestartWindow {
					ts.ptyRestartSince = time.Now()
					ts.ptyRestartCount = 0
				}
				ts.ptyRestartCount++
				if ts.ptyRestartCount > ptyRestartMax {
					shouldRestart = false
					ts.Running = false
					// Mark as detached (tmux session may still be alive)
					ts.Detached = true
					ts.ptyRestartBackoff = 0
				} else {
					backoff = ts.ptyRestartBackoff
					if backoff <= 0 {
						backoff = 200 * time.Millisecond
					} else {
						backoff *= 2
						if backoff > 5*time.Second {
							backoff = 5 * time.Second
						}
					}
					ts.ptyRestartBackoff = backoff
				}
				ts.mu.Unlock()
				if shouldRestart {
					restartTab := msg.TabID
					restartWt := msg.WorkspaceID
					cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
						return messages.SidebarPTYRestart{WorkspaceID: restartWt, TabID: restartTab}
					}))
					logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restarting in %s: %v", wsID, tabID, backoff, msg.Err)
				} else {
					logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restart limit reached, marking detached: %v", wsID, tabID, msg.Err)
				}
			} else {
				ts.mu.Lock()
				ts.Running = false
				// Mark as detached - tmux session may still be alive
				ts.Detached = true
				ts.ptyRestartBackoff = 0
				ts.ptyRestartCount = 0
				ts.ptyRestartSince = time.Time{}
				ts.mu.Unlock()
				logging.Info("Sidebar PTY stopped for workspace %s tab %s, marking detached: %v", wsID, tabID, msg.Err)
			}
		}

	case messages.SidebarPTYRestart:
		tab := m.getTabByID(msg.WorkspaceID, TerminalTabID(msg.TabID))
		if tab == nil || tab.State == nil {
			break
		}
		ts := tab.State
		if ts.Terminal == nil || ts.Terminal.IsClosed() {
			ts.mu.Lock()
			ts.ptyRestartBackoff = 0
			ts.mu.Unlock()
			break
		}
		if cmd := m.startPTYReader(msg.WorkspaceID, tab.ID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalCreated:
		cmd := m.HandleTerminalCreated(msg.WorkspaceID, msg.TabID, msg.Terminal, msg.SessionName)
		cmds = append(cmds, cmd)

	case sidebarTerminalReattachResult:
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab == nil || tab.State == nil {
			break
		}
		ts := tab.State
		termWidth, termHeight := m.terminalContentSize()
		ts.mu.Lock()
		if ts.VTerm == nil {
			ts.VTerm = vterm.New(termWidth, termHeight)
		}
		ts.Terminal = msg.Terminal
		ts.Running = true
		ts.Detached = false
		ts.SessionName = msg.SessionName
		ts.pendingOutput = nil
		ts.mu.Unlock()
		if msg.Terminal != nil {
			ts.VTerm.SetResponseWriter(func(data []byte) {
				_, _ = msg.Terminal.Write(data)
			})
			_ = msg.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
		}
		if cmd := m.startPTYReader(msg.WorkspaceID, tab.ID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case sidebarTerminalReattachFailed:
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			ts.mu.Lock()
			ts.Running = false
			if msg.Stopped {
				ts.Detached = false
			}
			ts.mu.Unlock()
		}
		action := msg.Action
		if action == "" {
			action = "reattach"
		}
		label := "Reattach"
		if action == "restart" {
			label = "Restart"
		}
		cmds = append(cmds, func() tea.Msg {
			return messages.Toast{Message: fmt.Sprintf("%s failed: %v", label, msg.Err), Level: messages.ToastWarning}
		})

	case SidebarTerminalCreateFailed:
		// Clear pending flag so user can retry
		delete(m.pendingCreation, msg.WorkspaceID)
		logging.Error("Failed to create sidebar terminal: %v", msg.Err)
		// Surface error to user via app-level error handling
		cmds = append(cmds, func() tea.Msg {
			return messages.Error{Err: msg.Err, Context: "creating sidebar terminal"}
		})

	case messages.WorkspaceDeleted:
		if msg.Workspace != nil {
			wsID := string(msg.Workspace.ID())
			tabs := m.tabsByWorkspace[wsID]
			for _, tab := range tabs {
				if tab.State != nil {
					m.stopPTYReader(tab.State)
					tab.State.mu.Lock()
					if tab.State.Terminal != nil {
						tab.State.Terminal.Close()
					}
					tab.State.Running = false
					tab.State.ptyRestartBackoff = 0
					tab.State.mu.Unlock()
				}
			}
			delete(m.tabsByWorkspace, wsID)
			delete(m.activeTabByWorkspace, wsID)
			delete(m.pendingCreation, wsID)
		}
	}

	return m, common.SafeBatch(cmds...)
}

// View renders the terminal section
func (m *TerminalModel) View() string {
	var b strings.Builder

	// Always render tab bar (shows "New terminal" when no tabs exist)
	tabBar := m.renderTabBar()
	if tabBar != "" {
		b.WriteString(tabBar)
		b.WriteString("\n")
	}

	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		// Show placeholder when no terminal
		if len(m.getTabs()) == 0 {
			// Empty state - tab bar already shows "New terminal" button
		} else {
			placeholder := m.styles.Muted.Render("No terminal")
			b.WriteString(placeholder)
		}
	} else {
		ts.mu.Lock()
		ts.VTerm.ShowCursor = m.focused
		// Use VTerm.Render() directly - it uses dirty line caching and delta styles
		content := ts.VTerm.Render()
		isScrolled := ts.VTerm.IsScrolled()
		var scrollInfo string
		if isScrolled {
			offset, total := ts.VTerm.GetScrollInfo()
			scrollInfo = formatScrollPos(offset, total)
		}
		ts.mu.Unlock()

		b.WriteString(content)

		if isScrolled {
			b.WriteString("\n")
			scrollStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(common.ColorBackground).
				Background(common.ColorInfo)
			b.WriteString(scrollStyle.Render(" SCROLL: " + scrollInfo + " "))
		}
	}

	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLinesForLayout(contentWidth)

	// Pad to fill height
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - len(helpLines) // Account for help
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(strings.Join(helpLines, "\n"))

	// Ensure output doesn't exceed m.height lines
	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}
