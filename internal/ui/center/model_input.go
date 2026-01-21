package center

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	defer perf.Time("center_update")()
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		// Handle tab bar clicks (e.g., the plus button) even without an active agent.
		if msg.Button == tea.MouseLeft {
			if cmd := m.handleTabBarClick(msg); cmd != nil {
				return m, cmd
			}
		}

		// Handle mouse events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		// Convert screen coordinates to terminal coordinates
		termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

		tab.mu.Lock()
		// Clear any existing selection first
		if tab.Terminal != nil {
			tab.Terminal.ClearSelection()
		}

		if inBounds {
			// Start new selection
			tab.Selection = SelectionState{
				Active: true,
				StartX: termX,
				StartY: termY,
				EndX:   termX,
				EndY:   termY,
			}
			if tab.Terminal != nil {
				tab.Terminal.SetSelection(termX, termY, termX, termY, true, false)
			}
			logging.Debug("Selection started at (%d, %d)", termX, termY)
		} else {
			// Clicked outside terminal content, just clear selection
			tab.Selection = SelectionState{}
		}
		tab.mu.Unlock()
		return m, tea.Batch(cmds...)

	case tea.MouseMotionMsg:
		// Handle mouse drag events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)

		// Update selection while dragging
		tab.mu.Lock()
		if tab.Selection.Active {
			termWidth := m.contentWidth()
			termHeight := m.height - 6
			if termWidth < 10 {
				termWidth = 80
			}
			if termHeight < 5 {
				termHeight = 24
			}

			// Clamp to terminal bounds
			if termX < 0 {
				termX = 0
			}
			if termY < 0 {
				termY = 0
			}
			if termX >= termWidth {
				termX = termWidth - 1
			}
			if termY >= termHeight {
				termY = termHeight - 1
			}

			tab.Selection.EndX = termX
			tab.Selection.EndY = termY
			if tab.Terminal != nil {
				tab.Terminal.SetSelection(
					tab.Selection.StartX, tab.Selection.StartY,
					termX, termY, true, false,
				)
			}
		}
		tab.mu.Unlock()
		return m, tea.Batch(cmds...)

	case tea.MouseReleaseMsg:
		// Handle mouse release events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		tab.mu.Lock()
		if tab.Selection.Active {
			// Extract selected text and copy to clipboard
			if tab.Terminal != nil {
				text := tab.Terminal.GetSelectedText(
					tab.Selection.StartX, tab.Selection.StartY,
					tab.Selection.EndX, tab.Selection.EndY,
				)
				if text != "" {
					if err := common.CopyToClipboard(text); err != nil {
						logging.Error("Failed to copy to clipboard: %v", err)
					} else {
						logging.Info("Copied %d chars to clipboard", len(text))
					}
				}
				// Keep selection visible - don't clear it
				// Selection will be cleared when user clicks again or types
			}
			// Mark selection as no longer being dragged, but keep it visible
			tab.Selection.Active = false
		}
		tab.mu.Unlock()
		return m, tea.Batch(cmds...)

	case tea.MouseWheelMsg:
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()

		// CommitViewer tabs: forward mouse events to commit viewer
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		// Terminal scroll: use viewport-proportional delta
		tab.mu.Lock()
		if tab.Terminal != nil {
			delta := common.ScrollDeltaForHeight(tab.Terminal.Height, 8)
			if msg.Button == tea.MouseWheelUp {
				tab.Terminal.ScrollView(delta)
			} else if msg.Button == tea.MouseWheelDown {
				tab.Terminal.ScrollView(-delta)
			}
		}
		tab.mu.Unlock()
		return m, nil

	case tea.PasteMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			if !m.focused {
				return m, nil
			}
			if tab.Agent != nil && tab.Agent.Terminal != nil {
				bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
				_ = tab.Agent.Terminal.SendString(bracketedText)
				logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
				return m, nil
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		logging.Debug("Center received key: %s, focused=%v, hasTabs=%v, numTabs=%d",
			msg.String(), m.focused, m.hasActiveAgent(), len(tabs))

		// Clear any selection when user types (unless in copy mode)
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			tab.mu.Lock()
			if !tab.CopyMode {
				if tab.Terminal != nil {
					tab.Terminal.ClearSelection()
				}
				tab.Selection = SelectionState{}
			}
			tab.mu.Unlock()
		}

		if !m.focused {
			logging.Debug("Center not focused, ignoring key")
			return m, nil
		}

		// When we have an active agent, handle keys
		if m.hasActiveAgent() {
			tab := tabs[activeIdx]
			logging.Debug("Has active agent, Agent=%v, Terminal=%v, CopyMode=%v", tab.Agent != nil, tab.Agent != nil && tab.Agent.Terminal != nil, tab.CopyMode)

			// CommitViewer tabs: forward keys to commit viewer
			tab.mu.Lock()
			cv := tab.CommitViewer
			tab.mu.Unlock()
			if cv != nil {
				// Handle ctrl+w for closing tab
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))) {
					return m, m.closeCurrentTab()
				}
				// Handle ctrl+n/p for tab switching
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))) {
					m.nextTab()
					return m, nil
				}
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))) {
					m.prevTab()
					return m, nil
				}
				// Forward all other keys to commit viewer
				newCV, cmd := cv.Update(msg)
				tab.mu.Lock()
				tab.CommitViewer = newCV
				tab.mu.Unlock()
				return m, cmd
			}

			// Copy mode: handle scroll navigation without sending to PTY
			if tab.CopyMode {
				return m, m.handleCopyModeKey(tab, msg)
			}

			if tab.Agent != nil && tab.Agent.Terminal != nil {
				// Only intercept these specific keys - everything else goes to terminal
				switch {
				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))):
					m.nextTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))):
					m.prevTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
					// Close tab
					return m, m.closeCurrentTab()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+]"))):
					// Switch to next tab (escape hatch that won't conflict)
					m.nextTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))):
					// This is Escape - let it go to terminal
					_ = tab.Agent.Terminal.SendString("\x1b")
					return m, nil
				}

				// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
				switch msg.Key().Code {
				case tea.KeyPgUp:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 4)
					}
					tab.mu.Unlock()
					return m, nil

				case tea.KeyPgDown:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
					}
					tab.mu.Unlock()
					return m, nil
				}

				// If scrolled, any typing goes back to live and sends key
				tab.mu.Lock()
				if tab.Terminal != nil && tab.Terminal.IsScrolled() {
					tab.Terminal.ScrollViewToBottom()
				}
				tab.mu.Unlock()

				// Forward ALL keys to terminal (no Ctrl interceptions)
				input := common.KeyToBytes(msg)
				if len(input) > 0 {
					logging.Debug("Sending to terminal: %q (len=%d)", input, len(input))
					_ = tab.Agent.Terminal.SendString(string(input))
				} else {
					logging.Debug("keyToBytes returned empty for: %s", msg.String())
				}
				return m, nil
			}
		}

	case messages.LaunchAgent:
		return m, m.createAgentTab(msg.Assistant, msg.Worktree)

	case messages.OpenDiff:
		return m, m.createViewerTab(msg.File, msg.StatusCode, msg.Worktree)

	case messages.OpenCommitViewer:
		return m, m.createCommitViewerTab(msg.Worktree)

	case messages.ViewCommitDiff:
		return m, m.createCommitDiffTab(msg.Hash, msg.Worktree)

	case messages.WorktreeDeleted:
		m.CleanupWorktree(msg.Worktree)
		return m, nil

	case PTYOutput:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			m.tracePTYOutput(tab, msg.Data)
			tab.pendingOutput = append(tab.pendingOutput, msg.Data...)
			perf.Count("pty_output_bytes", int64(len(msg.Data)))
			tab.lastOutputAt = time.Now()
			if !tab.flushScheduled {
				tab.flushScheduled = true
				tab.flushPendingSince = tab.lastOutputAt
				quiet, _ := m.flushTiming(tab, m.isActiveTab(msg.WorktreeID, msg.TabID))
				tabID := msg.TabID // Capture for closure
				cmds = append(cmds, tea.Tick(quiet, func(t time.Time) tea.Msg {
					return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
				}))
			}
			// Continue reading
			cmds = append(cmds, m.readPTYForTab(msg.WorktreeID, msg.TabID))
		}
		// If tab is nil, it was closed - silently drop the message and don't reschedule

	case PTYFlush:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			now := time.Now()
			quietFor := now.Sub(tab.lastOutputAt)
			pendingFor := time.Duration(0)
			if !tab.flushPendingSince.IsZero() {
				pendingFor = now.Sub(tab.flushPendingSince)
			}
			quiet, maxInterval := m.flushTiming(tab, m.isActiveTab(msg.WorktreeID, msg.TabID))
			if quietFor < quiet && pendingFor < maxInterval {
				delay := quiet - quietFor
				if delay < time.Millisecond {
					delay = time.Millisecond
				}
				tabID := msg.TabID
				tab.flushScheduled = true
				cmds = append(cmds, tea.Tick(delay, func(t time.Time) tea.Msg {
					return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
				}))
				break
			}

			tab.flushScheduled = false
			tab.flushPendingSince = time.Time{}
			if len(tab.pendingOutput) > 0 {
				tab.mu.Lock()
				if tab.Terminal != nil {
					chunkSize := len(tab.pendingOutput)
					if chunkSize > ptyFlushChunkSize {
						chunkSize = ptyFlushChunkSize
					}
					flushDone := perf.Time("pty_flush")
					tab.Terminal.Write(tab.pendingOutput[:chunkSize])
					flushDone()
					perf.Count("pty_flush_bytes", int64(chunkSize))
					copy(tab.pendingOutput, tab.pendingOutput[chunkSize:])
					tab.pendingOutput = tab.pendingOutput[:len(tab.pendingOutput)-chunkSize]
				}
				tab.mu.Unlock()
				if len(tab.pendingOutput) == 0 {
					tab.pendingOutput = tab.pendingOutput[:0]
				} else {
					tab.flushScheduled = true
					tab.flushPendingSince = time.Now()
					tabID := msg.TabID
					cmds = append(cmds, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
						return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
					}))
				}
			}
		}

	case PTYTick:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			cmds = append(cmds, m.readPTYForTab(msg.WorktreeID, msg.TabID))
		}
		// If tab is nil, it was closed - stop polling

	case PTYStopped:
		// Terminal closed - mark tab as not running, but keep it visible
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			tab.Running = false
			m.stopPTYReader(tab)
			logging.Info("PTY stopped for tab %s: %v", msg.TabID, msg.Err)
		}
		// Do NOT schedule another read - the loop is done

	default:
		// Forward unknown messages to active commit viewer if one exists
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			tab.mu.Lock()
			cv := tab.CommitViewer
			tab.mu.Unlock()
			if cv != nil {
				newCV, cmd := cv.Update(msg)
				tab.mu.Lock()
				tab.CommitViewer = newCV
				tab.mu.Unlock()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}
