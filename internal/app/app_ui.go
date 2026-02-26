package app

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// focusPane changes focus to the specified pane
func (a *App) focusPane(pane messages.PaneType) tea.Cmd {
	a.focusedPane = pane
	switch pane {
	case messages.PaneDashboard:
		a.dashboard.Focus()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	case messages.PaneCenter:
		a.dashboard.Blur()
		a.center.Focus()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
		// Seamless UX: when center regains focus, attempt reattach for detached active tab.
		return a.center.ReattachActiveTabIfDetached()
	case messages.PaneSidebar:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Focus()
		a.sidebarTerminal.Blur()
	case messages.PaneSidebarTerminal:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Focus()
		// Lazy initialization: create terminal on focus if none exists
		return a.sidebarTerminal.EnsureTerminalTab()
	}
	return nil
}

type prefixMatch int

const (
	prefixMatchNone prefixMatch = iota
	prefixMatchPartial
	prefixMatchComplete
)

type prefixCommand struct {
	Sequence []string
	Desc     string
	Action   string
}

// Prefix mode helpers (leader key)

// isPrefixKey returns true if the key is the prefix key
func (a *App) isPrefixKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, a.keymap.Prefix)
}

// enterPrefix enters prefix mode and schedules a timeout
func (a *App) enterPrefix() tea.Cmd {
	a.prefixActive = true
	a.prefixSequence = nil
	return a.refreshPrefixTimeout()
}

func (a *App) refreshPrefixTimeout() tea.Cmd {
	a.prefixToken++
	token := a.prefixToken
	return common.SafeTick(prefixTimeout, func(t time.Time) tea.Msg {
		return prefixTimeoutMsg{token: token}
	})
}

// exitPrefix exits prefix mode
func (a *App) exitPrefix() {
	a.prefixActive = false
	a.prefixSequence = nil
}

// handlePrefixCommand handles a key press while in prefix mode
// Returns (match state, cmd).
func (a *App) handlePrefixCommand(msg tea.KeyPressMsg) (prefixMatch, tea.Cmd) {
	token, ok := a.prefixInputToken(msg)
	if !ok {
		return prefixMatchNone, nil
	}

	if token == "backspace" {
		if len(a.prefixSequence) > 0 {
			a.prefixSequence = a.prefixSequence[:len(a.prefixSequence)-1]
		}
		return prefixMatchPartial, nil
	}

	a.prefixSequence = append(a.prefixSequence, token)

	if len(a.prefixSequence) == 1 {
		if r := []rune(token); len(r) == 1 && r[0] >= '1' && r[0] <= '9' {
			return prefixMatchComplete, a.prefixSelectTab(int(r[0] - '1'))
		}
	}

	matches := a.matchingPrefixCommands(a.prefixSequence)
	if len(matches) == 0 {
		return prefixMatchNone, nil
	}

	var exact *prefixCommand
	exactCount := 0
	for i := range matches {
		if len(matches[i].Sequence) == len(a.prefixSequence) {
			exactCount++
			exact = &matches[i]
		}
	}
	// Execute only when the sequence resolves to a unique leaf command.
	// Ambiguous prefixes intentionally stay in narrowing mode.
	if exactCount == 1 && len(matches) == 1 && exact != nil {
		return prefixMatchComplete, a.runPrefixAction(exact.Action)
	}

	return prefixMatchPartial, nil
}

func (a *App) prefixInputToken(msg tea.KeyPressMsg) (string, bool) {
	switch msg.Key().Code {
	case tea.KeyBackspace, tea.KeyDelete:
		return "backspace", true
	case tea.KeyLeft:
		return "h", true
	case tea.KeyDown:
		return "j", true
	case tea.KeyUp:
		return "k", true
	case tea.KeyRight:
		return "l", true
	}
	text := msg.Key().Text
	runes := []rune(text)
	if len(runes) != 1 {
		return "", false
	}
	return text, true
}

func (a *App) prefixCommands() []prefixCommand {
	return []prefixCommand{
		{Sequence: []string{"h"}, Desc: "focus left", Action: "move_left"},
		{Sequence: []string{"j"}, Desc: "focus down", Action: "move_down"},
		{Sequence: []string{"k"}, Desc: "focus up", Action: "move_up"},
		{Sequence: []string{"l"}, Desc: "focus right", Action: "move_right"},
		{Sequence: []string{"?"}, Desc: "toggle help", Action: "help"},
		{Sequence: []string{"q"}, Desc: "quit", Action: "quit"},
		{Sequence: []string{"K"}, Desc: "cleanup tmux", Action: "cleanup_tmux"},
		{Sequence: []string{"t", "a"}, Desc: "new agent tab", Action: "new_agent_tab"},
		{Sequence: []string{"t", "t"}, Desc: "new terminal tab", Action: "new_terminal_tab"},
		{Sequence: []string{"t", "n"}, Desc: "next tab", Action: "next_tab"},
		{Sequence: []string{"t", "p"}, Desc: "prev tab", Action: "prev_tab"},
		{Sequence: []string{"t", "x"}, Desc: "close tab", Action: "close_tab"},
		{Sequence: []string{"t", "d"}, Desc: "detach tab", Action: "detach_tab"},
		{Sequence: []string{"t", "r"}, Desc: "reattach tab", Action: "reattach_tab"},
		{Sequence: []string{"t", "s"}, Desc: "restart tab", Action: "restart_tab"},
	}
}

func (a *App) matchingPrefixCommands(sequence []string) []prefixCommand {
	commands := a.prefixCommands()
	if len(sequence) == 0 {
		return commands
	}

	matches := make([]prefixCommand, 0, len(commands))
	for _, cmd := range commands {
		if len(sequence) > len(cmd.Sequence) {
			continue
		}
		ok := true
		for i := range sequence {
			if cmd.Sequence[i] != sequence[i] {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, cmd)
		}
	}
	return matches
}

func (a *App) runPrefixAction(action string) tea.Cmd {
	switch action {
	case "move_left":
		switch a.focusedPane {
		case messages.PaneCenter:
			return a.focusPane(messages.PaneDashboard)
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
			return a.focusPane(messages.PaneCenter)
		}
		return nil
	case "move_right":
		switch a.focusedPane {
		case messages.PaneDashboard:
			return a.focusPane(messages.PaneCenter)
		case messages.PaneCenter:
			if a.layout.ShowSidebar() {
				return a.focusPane(messages.PaneSidebar)
			}
		}
		return nil
	case "move_up":
		if a.focusedPane == messages.PaneSidebarTerminal {
			return a.focusPane(messages.PaneSidebar)
		}
		return nil
	case "move_down":
		if a.focusedPane == messages.PaneSidebar && a.layout.ShowSidebar() {
			return a.focusPane(messages.PaneSidebarTerminal)
		}
		return nil
	case "help":
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return nil
	case "quit":
		a.showQuitDialog()
		return nil
	case "cleanup_tmux":
		return func() tea.Msg { return messages.ShowCleanupTmuxDialog{} }
	case "new_agent_tab":
		if a.activeWorkspace != nil {
			if !a.tmuxAvailable {
				return a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
			}
			return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
		return nil
	case "new_terminal_tab":
		if a.activeWorkspace != nil {
			if !a.tmuxAvailable {
				return a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
			}
			// Intentionally global to the workspace (no sidebar focus required).
			return a.sidebarTerminal.CreateNewTab()
		}
		return nil
	case "next_tab":
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.NextTab()
		case messages.PaneSidebar:
			a.sidebar.NextTab()
		default:
			_, activeIdxBefore := a.center.GetTabsInfo()
			cmd := a.center.NextTab()
			_, activeIdxAfter := a.center.GetTabsInfo()
			if activeIdxAfter == activeIdxBefore {
				return nil
			}
			return common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
		}
		return nil
	case "prev_tab":
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.PrevTab()
		case messages.PaneSidebar:
			a.sidebar.PrevTab()
		default:
			_, activeIdxBefore := a.center.GetTabsInfo()
			cmd := a.center.PrevTab()
			_, activeIdxAfter := a.center.GetTabsInfo()
			if activeIdxAfter == activeIdxBefore {
				return nil
			}
			return common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
		}
		return nil
	case "close_tab":
		if a.focusedPane == messages.PaneSidebarTerminal {
			return a.sidebarTerminal.CloseActiveTab()
		}
		return a.center.CloseActiveTab()
	case "detach_tab":
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.DetachActiveTab()
			return common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.DetachActiveTab()
		}
		return nil
	case "reattach_tab":
		switch a.focusedPane {
		case messages.PaneCenter:
			return a.center.ReattachActiveTab()
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.ReattachActiveTab()
		}
		return nil
	case "restart_tab":
		switch a.focusedPane {
		case messages.PaneCenter:
			return a.center.RestartActiveTab()
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.RestartActiveTab()
		}
		return nil
	default:
		return nil
	}
}

func (a *App) prefixSelectTab(index int) tea.Cmd {
	tabs, activeIdx := a.center.GetTabsInfo()
	if index < 0 || index >= len(tabs) || index == activeIdx {
		return nil
	}
	cmd := a.center.SelectTab(index)
	return common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
}

// sendPrefixToTerminal sends a literal Ctrl-Space (NUL) to the focused terminal
func (a *App) sendPrefixToTerminal() {
	if a.focusedPane == messages.PaneCenter {
		a.center.SendToTerminal("\x00")
	} else if a.focusedPane == messages.PaneSidebarTerminal {
		a.sidebarTerminal.SendToTerminal("\x00")
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())

	centerWidth := a.layout.CenterWidth()
	a.center.SetSize(centerWidth, a.layout.Height())
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	a.center.SetOffset(leftGutter + a.layout.DashboardWidth() + gapX) // Set X offset for mouse coordinate conversion
	a.center.SetCanFocusRight(a.layout.ShowSidebar())
	a.dashboard.SetCanFocusRight(a.layout.ShowCenter())

	// New two-pane sidebar structure: each pane has its own border
	sidebarWidth := a.layout.SidebarWidth()
	sidebarHeight := a.layout.Height()

	// Each pane gets half the height (borders touch)
	topPaneHeight, bottomPaneHeight := sidebarPaneHeights(sidebarHeight)

	// Content dimensions inside each pane (subtract border + padding)
	// Border: 2 (top + bottom), Padding: 2 (left + right from Pane style)
	contentWidth := sidebarWidth - 2 - 2 // border + padding
	if contentWidth < 1 {
		contentWidth = 1
	}
	topContentHeight := topPaneHeight - 2 // border only (no vertical padding in Pane style)
	if topContentHeight < 1 {
		topContentHeight = 1
	}
	bottomContentHeight := bottomPaneHeight - 2
	if bottomContentHeight < 1 {
		bottomContentHeight = 1
	}

	a.sidebar.SetSize(contentWidth, topContentHeight)
	a.sidebarTerminal.SetSize(contentWidth, bottomContentHeight)

	// Calculate and set offsets for sidebar mouse handling
	// X: Dashboard + Center + Border(1) + Padding(1)
	sidebarX := leftGutter + a.layout.DashboardWidth()
	if a.layout.ShowCenter() {
		sidebarX += a.layout.GapX() + a.layout.CenterWidth()
	}
	if a.layout.ShowSidebar() {
		sidebarX += a.layout.GapX()
	}
	sidebarContentOffsetX := sidebarX + 2 // +2 for border and padding

	// Y: Top pane height (including its border) + Bottom pane border(1)
	termOffsetY := topGutter + topPaneHeight + 1
	a.sidebarTerminal.SetOffset(sidebarContentOffsetX, termOffsetY)

	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
	if a.filePicker != nil {
		a.filePicker.SetSize(a.width, a.height)
	}
	if a.settingsDialog != nil {
		a.settingsDialog.SetSize(a.width, a.height)
	}
}

func (a *App) setKeymapHintsEnabled(enabled bool) {
	if a.config != nil {
		a.config.UI.ShowKeymapHints = enabled
	}
	a.dashboard.SetShowKeymapHints(enabled)
	a.center.SetShowKeymapHints(enabled)
	a.sidebar.SetShowKeymapHints(enabled)
	a.sidebarTerminal.SetShowKeymapHints(enabled)
	if a.dialog != nil {
		a.dialog.SetShowKeymapHints(enabled)
	}
	if a.filePicker != nil {
		a.filePicker.SetShowKeymapHints(enabled)
	}
}

func sidebarPaneHeights(total int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	top := total / 2
	bottom := total - top

	// Prefer keeping both panes visible when there's room.
	if total >= 6 {
		if top < 3 {
			top = 3
			bottom = total - top
		}
		if bottom < 3 {
			bottom = 3
			top = total - bottom
		}
		return top, bottom
	}

	// In tight spaces, keep the terminal visible by shrinking the top pane first.
	if total >= 3 && bottom < 3 {
		bottom = 3
		top = total - bottom
		if top < 0 {
			top = 0
		}
		return top, bottom
	}

	if top > total {
		top = total
	}
	if bottom < 0 {
		bottom = 0
	}
	return top, bottom
}
