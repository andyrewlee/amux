package app

import (
	"fmt"
	"runtime/debug"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) safeCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in command: %v\n%s", r, debug.Stack())
				msg = messages.Error{Err: fmt.Errorf("command panic: %v", r), Context: "command", Logged: true}
			}
		}()
		return cmd()
	}
}

func (a *App) safeBatch(cmds ...tea.Cmd) tea.Cmd {
	if len(cmds) == 0 {
		return nil
	}
	safe := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		safe = append(safe, a.safeCmd(cmd))
	}
	if len(safe) == 0 {
		return nil
	}
	return tea.Batch(safe...)
}

// syncActiveWorkspacesToDashboard syncs the active workspace state from center to dashboard.
// This ensures the dashboard has current data for spinner state decisions.
func (a *App) syncActiveWorkspacesToDashboard() {
	activeWorkspaces := make(map[string]bool)
	for wsID := range a.tmuxActiveWorkspaceIDs {
		activeWorkspaces[wsID] = true
	}
	a.dashboard.SetActiveWorkspaces(activeWorkspaces)
}

// handleKeyPress handles keyboard input
func (a *App) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	// Dismiss error on any key
	if a.err != nil {
		a.err = nil
		return nil
	}

	// 1. Handle prefix key (Ctrl+Space)
	if a.isPrefixKey(msg) {
		if a.prefixActive {
			// Prefix + Prefix = send literal Ctrl+Space to terminal
			a.sendPrefixToTerminal()
			a.exitPrefix()
			return nil
		}
		// Enter prefix mode
		return a.enterPrefix()
	}

	// 2. If prefix is active, handle mux commands
	if a.prefixActive {
		// Esc cancels prefix mode without forwarding
		code := msg.Key().Code
		if code == tea.KeyEsc || code == tea.KeyEscape {
			a.exitPrefix()
			return nil
		}

		handled, cmd := a.handlePrefixCommand(msg)
		if handled {
			a.exitPrefix()
			return cmd
		}
		// Unknown key in prefix mode: exit prefix and pass through
		a.exitPrefix()
		// Fall through to normal handling below
	}

	// 3. Passthrough mode - route keys to focused pane
	// Handle button navigation when center pane is focused and showing welcome/workspace info (no tabs)
	if a.focusedPane == messages.PaneCenter && !a.center.HasTabs() {
		maxIndex := a.centerButtonCount() - 1
		switch {
		case key.Matches(msg, a.keymap.Left), key.Matches(msg, a.keymap.Up):
			if a.centerBtnFocused {
				if a.centerBtnIndex > 0 {
					a.centerBtnIndex--
				} else {
					a.centerBtnFocused = false
				}
			} else {
				// Enter from the right/bottom - focus last button
				a.centerBtnFocused = true
				a.centerBtnIndex = maxIndex
			}
			return nil
		case key.Matches(msg, a.keymap.Right), key.Matches(msg, a.keymap.Down):
			if a.centerBtnFocused {
				if a.centerBtnIndex < maxIndex {
					a.centerBtnIndex++
				} else {
					a.centerBtnFocused = false
				}
			} else {
				// Enter from the left/top - focus first button
				a.centerBtnFocused = true
				a.centerBtnIndex = 0
			}
			return nil
		case key.Matches(msg, a.keymap.Enter):
			if a.centerBtnFocused {
				return a.activateCenterButton()
			}
		}
	}

	// Route to focused pane
	switch a.focusedPane {
	case messages.PaneDashboard:
		newDashboard, cmd := a.dashboard.Update(msg)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return cmd
	case messages.PaneSidebar:
		newSidebar, cmd := a.sidebar.Update(msg)
		a.sidebar = newSidebar
		return cmd
	case messages.PaneSidebarTerminal:
		newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newSidebarTerminal
		return cmd
	}
	return nil
}

func (a *App) handleKeyboardEnhancements(msg tea.KeyboardEnhancementsMsg) {
	a.keyboardEnhancements = msg
	logging.Info("Keyboard enhancements: disambiguation=%t event_types=%t", msg.SupportsKeyDisambiguation(), msg.SupportsEventTypes())
}

func (a *App) handleWindowSize(msg tea.WindowSizeMsg) {
	a.width = msg.Width
	a.height = msg.Height
	a.ready = true
	a.layout.Resize(msg.Width, msg.Height)
	a.updateLayout()
	// Update help overlay size for accurate hit-testing after resize
	if a.helpOverlay.Visible() {
		a.helpOverlay.SetSize(a.width, a.height)
	}
}

func (a *App) handlePaste(msg tea.PasteMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneCenter:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return cmd
	case messages.PaneSidebarTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	}
	return nil
}

func (a *App) handlePrefixTimeout(msg prefixTimeoutMsg) {
	if msg.token == a.prefixToken && a.prefixActive {
		a.exitPrefix()
	}
}

// centerButtonCount returns the number of buttons shown on the current center screen
func (a *App) centerButtonCount() int {
	if a.showWelcome {
		return 2 // [Add project], [Settings]
	}
	if a.activeWorkspace != nil {
		return 1 // [New agent]
	}
	return 0
}

// activateCenterButton activates the currently focused center button
func (a *App) activateCenterButton() tea.Cmd {
	if a.showWelcome {
		switch a.centerBtnIndex {
		case 0:
			return func() tea.Msg { return messages.ShowAddProjectDialog{} }
		case 1:
			return func() tea.Msg { return messages.ShowSettingsDialog{} }
		}
	} else if a.activeWorkspace != nil {
		return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
	}
	return nil
}
