package app

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// syncActiveWorkspacesToDashboard syncs the active workspace state from center to dashboard.
// This ensures the dashboard has current data for spinner state decisions.
func (a *App) syncActiveWorkspacesToDashboard() {
	if a.dashboard == nil {
		return
	}
	activeWorkspaces := make(map[string]bool)
	if !a.tmuxActivity.settled {
		a.dashboard.SetActiveWorkspaces(activeWorkspaces)
		a.dashboard.SetAgentStates(nil)
		return
	}
	for wsID := range a.tmuxActivity.activeWorkspaceIDs {
		// A scan completing after a delete began must not re-publish the workspace
		// as active; the delete-in-flight guard keeps the dashboard consistent.
		if a.isWorkspaceDeleteInFlight(wsID) {
			continue
		}
		activeWorkspaces[wsID] = true
	}
	a.dashboard.SetActiveWorkspaces(activeWorkspaces)
	a.dashboard.SetAgentStates(a.tmuxActivity.agentStates)
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
			if len(a.prefixSequence) == 0 {
				// Prefix + Prefix = send literal Ctrl+Space to terminal.
				a.sendPrefixToTerminal()
				a.exitPrefix()
				return nil
			}
			// Restart narrowing from the root command list.
			a.prefixSequence = nil
			return a.refreshPrefixTimeout()
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

		status, cmd := a.handlePrefixCommand(msg)
		switch status {
		case prefixMatchComplete:
			a.exitPrefix()
			return cmd
		case prefixMatchPartial:
			// Keep prefix mode open while the sequence narrows.
			return a.refreshPrefixTimeout()
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

// centerButton describes one focusable button on a tab-less center screen: the
// label rendered in the pane and the message its activation emits.
type centerButton struct {
	label string
	msg   tea.Msg
}

// centerButtonState selects which tab-less center screen's button set to use.
type centerButtonState int

const (
	// centerButtonsNone is a screen with no focusable buttons.
	centerButtonsNone centerButtonState = iota
	// centerButtonsWelcome is the welcome screen: [Add project], [Settings].
	centerButtonsWelcome
	// centerButtonsWorkspace is the active-workspace screen: [New agent].
	centerButtonsWorkspace
)

// centerButtonsFor is the single source of truth for the center pane's
// focusable buttons in a given tab-less state. The slice order matches both the
// rendered left-to-right (or top-to-bottom) layout and the centerBtnIndex used
// by keyboard navigation and activation. Renderers pass their own fixed state;
// the keyboard path resolves the current state via currentCenterButtonState.
func centerButtonsFor(state centerButtonState) []centerButton {
	switch state {
	case centerButtonsWelcome:
		return []centerButton{
			{label: "[Add project]", msg: messages.ShowAddProjectDialog{}},
			{label: "[Settings]", msg: messages.ShowSettingsDialog{}},
		}
	case centerButtonsWorkspace:
		return []centerButton{
			{label: "[New agent]", msg: messages.ShowSelectAssistantDialog{}},
		}
	}
	return nil
}

// currentCenterButtonState maps the live app flags to the active center button
// screen, mirroring the original showWelcome-then-activeWorkspace precedence.
func (a *App) currentCenterButtonState() centerButtonState {
	if a.showWelcome {
		return centerButtonsWelcome
	}
	if a.activeWorkspace != nil {
		return centerButtonsWorkspace
	}
	return centerButtonsNone
}

// centerButtonCount returns the number of buttons shown on the current center screen
func (a *App) centerButtonCount() int {
	return len(centerButtonsFor(a.currentCenterButtonState()))
}

// activateCenterButton activates the currently focused center button
func (a *App) activateCenterButton() tea.Cmd {
	buttons := centerButtonsFor(a.currentCenterButtonState())
	if a.centerBtnIndex < 0 || a.centerBtnIndex >= len(buttons) {
		return nil
	}
	msg := buttons[a.centerBtnIndex].msg
	return func() tea.Msg { return msg }
}
