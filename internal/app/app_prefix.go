package app

import (
	"errors"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

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
	commands := append([]prefixCommand(nil), prefixCommandTable...)
	if a.centerScrollPrefixActive() {
		commands = append(commands, prefixCommand{Sequence: []string{"u"}, Desc: "scroll up", Action: "scroll_up"})
		for i := range commands {
			if len(commands[i].Sequence) == 1 && commands[i].Sequence[0] == "d" {
				commands[i].Desc = "scroll down"
				commands[i].Action = "scroll_down"
				break
			}
		}
	}
	return commands
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
	case "scroll_up":
		if a.centerScrollPrefixActive() {
			a.center.ScrollActiveTerminalPage(1)
		}
		return nil
	case "scroll_down":
		if a.centerScrollPrefixActive() {
			a.center.ScrollActiveTerminalPage(-1)
		}
		return nil
	case "add_project":
		return func() tea.Msg { return messages.ShowAddProjectDialog{} }
	case "delete_workspace":
		return a.deleteWorkspaceCommand()
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
			return common.ReportError("creating agent tab", errors.New("tmux not available"), "tmux required to create tabs. "+a.tmuxInstallHint)
		}
		return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
	case "new_terminal_tab":
		if a.activeWorkspace == nil || a.activeProject == nil {
			return a.requireWorkspaceSelection("create terminal tab")
		}
		if !a.tmuxAvailable {
			return common.ReportError("creating terminal tab", errors.New("tmux not available"), "tmux required to create tabs. "+a.tmuxInstallHint)
		}
		// Intentionally global to the workspace (no sidebar focus required).
		return a.sidebarTerminal.CreateNewTab()
	case "next_tab":
		return a.cycleTab(a.sidebar.NextTab, a.sidebarTerminal.NextTab, a.center.NextTab)
	case "prev_tab":
		return a.cycleTab(a.sidebar.PrevTab, a.sidebarTerminal.PrevTab, a.center.PrevTab)
	case "close_tab":
		if a.focusedPane == messages.PaneSidebarTerminal {
			return a.sidebarTerminal.CloseActiveTab()
		}
		return a.center.CloseActiveTab()
	case "detach_tab":
		return a.dispatchTabAction(
			func() tea.Cmd { return common.SafeBatch(a.center.DetachActiveTab(), a.persistActiveWorkspaceTabs()) },
			a.sidebarTerminal.DetachActiveTab,
		)
	case "reattach_tab":
		return a.dispatchTabAction(a.center.ReattachActiveTab, a.sidebarTerminal.ReattachActiveTab)
	case "restart_tab":
		return a.dispatchTabAction(a.center.RestartActiveTab, a.sidebarTerminal.RestartActiveTab)
	default:
		return nil
	}
}

func (a *App) centerScrollPrefixActive() bool {
	return a != nil &&
		a.focusedPane == messages.PaneCenter &&
		a.center != nil &&
		a.center.HasActiveTerminal()
}

func (a *App) deleteWorkspaceCommand() tea.Cmd {
	if a.activeWorkspace == nil || a.activeProject == nil {
		return a.requireWorkspaceSelection("delete workspace")
	}
	return func() tea.Msg {
		return messages.ShowDeleteWorkspaceDialog{
			Project:   a.activeProject,
			Workspace: a.activeWorkspace,
		}
	}
}

// cycleTab handles next/prev tab for the focused pane, persisting center tab changes.
func (a *App) cycleTab(sidebarFn, sidebarTermFn func(), centerFn func() tea.Cmd) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneSidebarTerminal:
		sidebarTermFn()
	case messages.PaneSidebar:
		sidebarFn()
	default:
		_, before := a.center.GetTabsInfo()
		cmd := centerFn()
		_, after := a.center.GetTabsInfo()
		if after == before {
			return nil
		}
		return common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
	}
	return nil
}

// dispatchTabAction dispatches a tab action to center or sidebar terminal.
func (a *App) dispatchTabAction(centerFn, sidebarTermFn func() tea.Cmd) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneCenter:
		return centerFn()
	case messages.PaneSidebarTerminal:
		return sidebarTermFn()
	}
	return nil
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
