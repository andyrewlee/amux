package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Action   prefixAction
}

// prefixCommandTable is user-documented in README.md ("Controls") — add a
// row there when adding a command.
var prefixCommandTable = []prefixCommand{
	{Sequence: []string{"a"}, Desc: "add project", Action: prefixActionAddProject},
	{Sequence: []string{"d"}, Desc: "delete workspace", Action: prefixActionDeleteWorkspace},
	{Sequence: []string{"S"}, Desc: "Settings", Action: prefixActionOpenSettings},
	{Sequence: []string{"q"}, Desc: "quit", Action: prefixActionQuit},
	{Sequence: []string{"K"}, Desc: "cleanup tmux", Action: prefixActionCleanupTmux},
	{Sequence: []string{"h"}, Desc: "focus left", Action: prefixActionFocusLeft},
	{Sequence: []string{"l"}, Desc: "focus right", Action: prefixActionFocusRight},
	{Sequence: []string{"n"}, Desc: "next attention", Action: prefixActionNextAttention},
	{Sequence: []string{"t", "a"}, Desc: "new agent tab", Action: prefixActionNewAgentTab},
	{Sequence: []string{"t", "t"}, Desc: "new terminal tab", Action: prefixActionNewTerminalTab},
	{Sequence: []string{"t", "n"}, Desc: "next tab", Action: prefixActionNextTab},
	{Sequence: []string{"t", "p"}, Desc: "prev tab", Action: prefixActionPrevTab},
	{Sequence: []string{"t", "x"}, Desc: "close tab", Action: prefixActionCloseTab},
	{Sequence: []string{"t", "d"}, Desc: "detach tab", Action: prefixActionDetachTab},
	{Sequence: []string{"t", "r"}, Desc: "reattach tab", Action: prefixActionReattachTab},
	{Sequence: []string{"t", "s"}, Desc: "restart tab", Action: prefixActionRestartTab},
	{Sequence: []string{"t", "y"}, Desc: "copy transcript", Action: prefixActionCopyTranscript},
	{Sequence: []string{"t", "f"}, Desc: "save transcript to file", Action: prefixActionSaveTranscript},
	{Sequence: []string{"t", "o"}, Desc: "open saved transcript", Action: prefixActionBrowseScripts},
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
		commands = append(commands, prefixCommand{Sequence: []string{"u"}, Desc: "scroll up", Action: prefixActionScrollUp})
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

func (a *App) runPrefixAction(action prefixAction) tea.Cmd {
	switch action {
	case prefixActionFocusLeft:
		return a.focusPaneLeft()
	case prefixActionFocusRight:
		return a.focusPaneRight()
	case prefixActionNextAttention:
		return a.nextAttentionCommand()
	case prefixActionScrollUp:
		a.scrollActivePagePrefix(1)
		return nil
	case prefixActionScrollDown:
		a.scrollActivePagePrefix(-1)
		return nil
	case prefixActionAddProject:
		return func() tea.Msg { return messages.ShowAddProjectDialog{} }
	case prefixActionDeleteWorkspace:
		return a.deleteWorkspaceCommand()
	case prefixActionOpenSettings:
		return func() tea.Msg { return messages.ShowSettingsDialog{} }
	case prefixActionQuit:
		a.showQuitDialog()
		return nil
	case prefixActionCleanupTmux:
		return func() tea.Msg { return messages.ShowCleanupTmuxDialog{} }
	case prefixActionNewAgentTab:
		return a.newAgentTabCommand()
	case prefixActionNewTerminalTab:
		return a.newTerminalTabCommand()
	case prefixActionNextTab:
		return a.cycleTab(a.sidebar.NextTab, a.sidebarTerminal.NextTab, a.center.NextTab)
	case prefixActionPrevTab:
		return a.cycleTab(a.sidebar.PrevTab, a.sidebarTerminal.PrevTab, a.center.PrevTab)
	case prefixActionCloseTab:
		if a.focusedPane == messages.PaneSidebarTerminal {
			return a.sidebarTerminal.CloseActiveTab()
		}
		return a.center.CloseActiveTab()
	case prefixActionDetachTab:
		return a.dispatchTabAction(
			func() tea.Cmd { return common.SafeBatch(a.center.DetachActiveTab(), a.persistActiveWorkspaceTabs()) },
			a.sidebarTerminal.DetachActiveTab,
		)
	case prefixActionReattachTab:
		return a.dispatchTabAction(a.center.ReattachActiveTab, a.sidebarTerminal.ReattachActiveTab)
	case prefixActionRestartTab:
		return a.dispatchTabAction(a.center.RestartActiveTab, a.sidebarTerminal.RestartActiveTab)
	case prefixActionCopyTranscript:
		return a.copyTranscriptCommand()
	case prefixActionSaveTranscript:
		return a.saveTranscriptCommand()
	case prefixActionBrowseScripts:
		return a.browseTranscriptsCommand()
	default:
		return nil
	}
}

// nextAttentionCommand jumps the dashboard cursor to the next done-badge row
// and lands on it (activation rides along, so the ack + preview happen
// exactly as if the user had cursor'd there). Focus follows the dashboard so
// the jumped row is visibly selected.
func (a *App) nextAttentionCommand() tea.Cmd {
	if a.dashboard == nil {
		return nil
	}
	jump := a.dashboard.JumpToNextAttention()
	if jump == nil {
		return a.toast.ShowInfo("No workspaces need attention")
	}
	return common.SafeBatch(a.focusPane(messages.PaneDashboard), jump)
}

func (a *App) scrollActivePagePrefix(pages int) {
	if a.centerScrollPrefixActive() {
		a.center.ScrollActiveTerminalPage(pages)
	}
}

func (a *App) newAgentTabCommand() tea.Cmd {
	if a.activeWorkspace == nil || a.activeProject == nil {
		return a.requireWorkspaceSelection("create agent tab")
	}
	if !a.tmuxAvailable {
		return common.ReportError("creating agent tab", errors.New("tmux not available"), "tmux required to create tabs. "+a.tmuxInstallHint)
	}
	return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
}

func (a *App) newTerminalTabCommand() tea.Cmd {
	if a.activeWorkspace == nil || a.activeProject == nil {
		return a.requireWorkspaceSelection("create terminal tab")
	}
	if !a.tmuxAvailable {
		return common.ReportError("creating terminal tab", errors.New("tmux not available"), "tmux required to create tabs. "+a.tmuxInstallHint)
	}
	// Intentionally global to the workspace (no sidebar focus required).
	return a.sidebarTerminal.CreateNewTab()
}

// activeTranscript returns the focused terminal pane's full transcript
// (scrollback + screen), captured under the pane's lock by ActiveTranscript.
// Empty when the focused pane has no transcript (dashboard, empty pane).
func (a *App) activeTranscript() string {
	switch a.focusedPane {
	case messages.PaneCenter:
		if a.center != nil {
			return a.center.ActiveTranscript()
		}
	case messages.PaneSidebarTerminal:
		if a.sidebarTerminal != nil {
			return a.sidebarTerminal.ActiveTranscript()
		}
	}
	return ""
}

// copyTranscriptCommand copies the focused terminal pane's transcript to the
// clipboard. The clipboard write runs inside the returned cmd — off the Update
// goroutine and off any tab lock, per the CopyToClipboardE contract — and the
// toast reports the real result instead of pre-claiming success.
// User-initiated, so the OSC52 env gate does not apply.
func (a *App) copyTranscriptCommand() tea.Cmd {
	text := a.activeTranscript()
	if text == "" {
		return a.toast.ShowWarning("No transcript to copy")
	}
	text, truncated := common.TruncateTranscriptTail(text)
	copyFn := a.copyToClipboardFn
	if copyFn == nil {
		copyFn = common.CopyToClipboardE
	}
	return func() tea.Msg {
		if err := copyFn(text, "transcript"); err != nil {
			return messages.Toast{
				Message: fmt.Sprintf("Copy transcript failed: %v", err),
				Level:   messages.ToastError,
			}
		}
		if truncated {
			return messages.Toast{
				Message: fmt.Sprintf("Copied transcript — truncated to last %d chars", len(text)),
				Level:   messages.ToastSuccess,
			}
		}
		return messages.Toast{
			Message: fmt.Sprintf("Copied transcript — %d chars", len(text)),
			Level:   messages.ToastSuccess,
		}
	}
}

// saveTranscriptCommand opens an input dialog for a destination path and
// snapshots the focused pane's transcript NOW — the terminal keeps scrolling
// while the dialog is up, so the confirm handler writes the captured bytes,
// not a re-read. Unlike the clipboard path there is no size cap: a file sink
// can hold the full scrollback.
func (a *App) saveTranscriptCommand() tea.Cmd {
	text := a.activeTranscript()
	if text == "" {
		return a.toast.ShowWarning("No transcript to save")
	}
	a.requestOverlayOpen(func() {
		a.dlg.transcript = text
		name := "transcript"
		if a.activeWorkspace != nil && a.activeWorkspace.Name != "" {
			// ValidateWorkspaceName already restricts to [a-zA-Z0-9._-],
			// so the name is filename-safe as-is.
			name = a.activeWorkspace.Name
		}
		defaultPath := fmt.Sprintf("~/.amux/transcripts/%s-%s.txt",
			name, time.Now().UTC().Format("20060102-150405"))
		a.dialog = common.NewInputDialog(DialogSaveTranscript, "Save Transcript", "file path...")
		a.dialog.SetInputValidate(func(s string) string {
			if strings.TrimSpace(s) == "" {
				return "path cannot be empty"
			}
			return ""
		})
		a.presentDialog(a.dialog)
		// presentDialog → Show() resets the input; prefill after presenting.
		a.dialog.SetInputValue(defaultPath)
	})
	return nil
}

// browseTranscriptsCommand opens a file picker rooted at ~/.amux/transcripts
// — the dir saveTranscriptCommand pre-fills — so saved transcripts are
// reviewable without leaving amux. The chosen file opens in the configured
// file-viewer tab, which needs a workspace to host its tmux session.
func (a *App) browseTranscriptsCommand() tea.Cmd {
	if a.activeWorkspace == nil {
		return a.requireWorkspaceSelection("open transcript")
	}
	if a.config == nil || a.config.Paths == nil {
		return a.toast.ShowError("Open transcript: amux paths not initialized")
	}
	dir := filepath.Join(a.config.Paths.Home, "transcripts")
	entries, err := os.ReadDir(dir)
	hasFile := false
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				hasFile = true
				break
			}
		}
	}
	if !hasFile {
		return a.toast.ShowInfo("No transcripts saved yet")
	}
	a.requestOverlayOpen(func() {
		a.filePicker = common.NewFilePicker(DialogBrowseTranscripts, dir, false)
		a.filePicker.SetTitle("Open transcript")
		a.filePicker.SetPrimaryActionLabel("Open")
		a.presentFilePicker(a.filePicker)
	})
	if a.filePicker != nil && a.filePicker.Visible() {
		return a.filePicker.LoadCmd()
	}
	return nil
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
