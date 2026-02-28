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
	// Keep focus transitions fail-safe for partially initialized App instances
	// used in lightweight tests.
	a.syncPaneFocusFlags()
	switch pane {
	case messages.PaneCenter:
		// Seamless UX: when center regains focus, attempt reattach for detached active tab.
		if a.center != nil {
			return a.center.ReattachActiveTabIfDetached()
		}
	case messages.PaneSidebarTerminal:
		// Lazy initialization: create terminal on focus if none exists.
		if a.sidebarTerminal != nil {
			return a.sidebarTerminal.EnsureTerminalTab()
		}
	}
	return nil
}

// focusPaneLeft moves focus one pane to the left, respecting layout visibility.
func (a *App) focusPaneLeft() tea.Cmd {
	switch a.focusedPane {
	case messages.PaneSidebarTerminal, messages.PaneSidebar:
		if a.layout != nil && a.layout.ShowCenter() {
			return a.focusPane(messages.PaneCenter)
		}
		return a.focusPane(messages.PaneDashboard)
	case messages.PaneCenter:
		return a.focusPane(messages.PaneDashboard)
	}
	return nil
}

// focusPaneRight moves focus one pane to the right, respecting layout visibility.
func (a *App) focusPaneRight() tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		if a.layout != nil && a.layout.ShowCenter() {
			return a.focusPane(messages.PaneCenter)
		}
		if a.layout != nil && a.layout.ShowSidebar() {
			return a.focusPane(messages.PaneSidebar)
		}
	case messages.PaneCenter:
		if a.layout != nil && a.layout.ShowSidebar() {
			return a.focusPane(messages.PaneSidebar)
		}
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

var prefixCommandTable = []prefixCommand{
	{Sequence: []string{"a"}, Desc: "add project", Action: "add_project"},
	{Sequence: []string{"d"}, Desc: "delete workspace", Action: "delete_workspace"},
	{Sequence: []string{"S"}, Desc: "Settings", Action: "open_settings"},
	{Sequence: []string{"q"}, Desc: "quit", Action: "quit"},
	{Sequence: []string{"K"}, Desc: "cleanup tmux", Action: "cleanup_tmux"},
	{Sequence: []string{"h"}, Desc: "focus left", Action: "focus_left"},
	{Sequence: []string{"l"}, Desc: "focus right", Action: "focus_right"},
	{Sequence: []string{"t", "a"}, Desc: "new agent tab", Action: "new_agent_tab"},
	{Sequence: []string{"t", "t"}, Desc: "new terminal tab", Action: "new_terminal_tab"},
	{Sequence: []string{"t", "n"}, Desc: "next tab", Action: "next_tab"},
	{Sequence: []string{"t", "p"}, Desc: "prev tab", Action: "prev_tab"},
	{Sequence: []string{"t", "x"}, Desc: "close tab", Action: "close_tab"},
	{Sequence: []string{"t", "d"}, Desc: "detach tab", Action: "detach_tab"},
	{Sequence: []string{"t", "r"}, Desc: "reattach tab", Action: "reattach_tab"},
	{Sequence: []string{"t", "s"}, Desc: "restart tab", Action: "restart_tab"},
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

// openCommandsPalette opens (or resets) the bottom command palette.
// This message-driven path is used by mouse/toolbar interactions and therefore
// never sends a literal Ctrl-Space (NUL) to terminals.
func (a *App) openCommandsPalette() tea.Cmd {
	if !a.prefixActive {
		return a.enterPrefix()
	}
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
		// Keep the palette open at root so Backspace remains a harmless undo key.
		return prefixMatchPartial, nil
	}

	a.prefixSequence = append(a.prefixSequence, token)
	// Record the typed token before matching so the palette can render the
	// narrowed path immediately; unknown sequences still fall through to
	// prefixMatchNone below and exit prefix mode in handleKeyPress.

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
		// Some terminals report Backspace as KeyDelete; treat both as undo.
		return "backspace", true
	}
	text := msg.Key().Text
	runes := []rune(text)
	if len(runes) != 1 {
		return "", false
	}
	return text, true
}

func (a *App) prefixCommands() []prefixCommand {
	return prefixCommandTable
}

// matchingPrefixCommands intentionally does not apply prefixActionVisible.
// Command execution remains permissive and unavailable actions fail gracefully
// in runPrefixAction with contextual no-op/toast behavior.
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
	case "focus_left":
		return a.focusPaneLeft()
	case "focus_right":
		return a.focusPaneRight()
	case "add_project":
		return func() tea.Msg { return messages.ShowAddProjectDialog{} }
	case "delete_workspace":
		if a.activeWorkspace == nil || a.activeProject == nil {
			return a.requireWorkspaceSelection("delete workspace")
		}
		return func() tea.Msg {
			return messages.ShowDeleteWorkspaceDialog{
				Project:   a.activeProject,
				Workspace: a.activeWorkspace,
			}
		}
	case "open_settings":
		return func() tea.Msg { return messages.ShowSettingsDialog{} }
	case "quit":
		a.showQuitDialog()
		return nil
	case "cleanup_tmux":
		return func() tea.Msg { return messages.ShowCleanupTmuxDialog{} }
	case "new_agent_tab":
		if a.activeWorkspace == nil || a.activeProject == nil {
			return a.requireWorkspaceSelection("create agent tab")
		}
		if !a.tmuxAvailable {
			return a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
		}
		return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
	case "new_terminal_tab":
		if a.activeWorkspace == nil || a.activeProject == nil {
			return a.requireWorkspaceSelection("create terminal tab")
		}
		if !a.tmuxAvailable {
			return a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
		}
		// Intentionally global to the workspace (no sidebar focus required).
		return a.sidebarTerminal.CreateNewTab()
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

func (a *App) requireWorkspaceSelection(action string) tea.Cmd {
	if a.activeWorkspace != nil && a.activeProject != nil {
		return nil
	}
	if a.toast != nil {
		return a.toast.ShowWarning("Select a workspace before " + action)
	}
	return nil
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
