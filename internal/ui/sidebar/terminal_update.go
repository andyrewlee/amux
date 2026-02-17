package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	if ts.VTerm != nil && ts.VTerm.AltScreen {
		return ptyFlushQuietAlt, ptyFlushMaxAlt
	}
	return ptyFlushQuiet, ptyFlushMaxInterval
}

// Init initializes the terminal model
func (m *TerminalModel) Init() tea.Cmd {
	return nil
}

func (m *TerminalModel) Update(msg tea.Msg) (*TerminalModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case SidebarSelectionScrollTick:
		if cmd := m.handleSelectionScrollTick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
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
			m.detachState(ts, false)
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
				m.detachState(ts, false)
			}
		}

	case messages.SidebarPTYOutput:
		if cmd := m.handlePTYOutput(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYFlush:
		if cmd := m.handlePTYFlush(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYStopped:
		if cmd := m.handlePTYStopped(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYRestart:
		if cmd := m.handlePTYRestart(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalCreated:
		if cmd := m.handleTerminalCreated(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalReattachResult:
		if cmd := m.handleReattachResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalReattachFailed:
		if cmd := m.handleReattachFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalCreateFailed:
		if cmd := m.handleCreateFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceDeleted:
		if cmd := m.handleWorkspaceDeleted(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, common.SafeBatch(cmds...)
}
