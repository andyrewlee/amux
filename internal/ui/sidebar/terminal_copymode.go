package sidebar

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// EnterCopyMode enters copy/scroll mode for the current terminal
func (m *TerminalModel) EnterCopyMode() {
	ts := m.getTerminal()
	if ts != nil {
		ts.CopyMode = true
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.CopyState = common.InitCopyState(ts.VTerm)
		}
		ts.mu.Unlock()
	}
}

// ExitCopyMode exits copy/scroll mode for the current terminal
func (m *TerminalModel) ExitCopyMode() {
	ts := m.getTerminal()
	if ts != nil {
		ts.CopyMode = false
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.VTerm.ClearSelection()
			ts.VTerm.ScrollViewToBottom()
		}
		ts.CopyState = common.CopyState{}
		ts.mu.Unlock()
	}
}

// CopyModeActive returns whether the current terminal is in copy mode
func (m *TerminalModel) CopyModeActive() bool {
	ts := m.getTerminal()
	return ts != nil && ts.CopyMode
}

// handleCopyModeKey handles keys while in copy mode (scroll navigation)
func (m *TerminalModel) handleCopyModeKey(ts *TerminalState, msg tea.KeyPressMsg) tea.Cmd {
	var copyText string
	var didCopy bool

	ts.mu.Lock()
	term := ts.VTerm
	if term == nil {
		ts.mu.Unlock()
		return nil
	}

	k := msg.Key()
	if ts.CopyState.SearchActive {
		switch {
		case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
			common.CancelSearch(&ts.CopyState)
		case k.Code == tea.KeyEnter:
			common.ExecuteSearch(term, &ts.CopyState)
		case k.Code == tea.KeyBackspace:
			common.BackspaceSearchQuery(&ts.CopyState)
		default:
			if k.Text != "" && (k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper)) == 0 {
				common.AppendSearchQuery(&ts.CopyState, k.Text)
			}
		}
		ts.mu.Unlock()
		return nil
	}

	switch {
	// Exit copy mode
	case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
		fallthrough
	case msg.String() == "q":
		ts.CopyMode = false
		ts.CopyState = common.CopyState{}
		term.ClearSelection()
		term.ScrollViewToBottom()
		ts.mu.Unlock()
		return nil

	// Copy selection
	case k.Code == tea.KeyEnter || msg.String() == "y":
		copyText = common.CopySelectionText(term, &ts.CopyState)
		if copyText != "" {
			ts.CopyMode = false
			ts.CopyState = common.CopyState{}
			term.ClearSelection()
			term.ScrollViewToBottom()
			didCopy = true
		}
		ts.mu.Unlock()
		if didCopy {
			if err := common.CopyToClipboard(copyText); err != nil {
				logging.Error("Failed to copy sidebar selection: %v", err)
			} else {
				logging.Info("Copied %d chars from sidebar", len(copyText))
			}
		}
		return nil

	// Toggle selection
	case msg.String() == " " || msg.String() == "v":
		common.ToggleCopySelection(&ts.CopyState)
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Toggle rectangle selection
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+v"))):
		common.ToggleRectangle(&ts.CopyState)
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Search
	case msg.String() == "/":
		common.StartSearch(&ts.CopyState, false)
		ts.mu.Unlock()
		return nil

	case msg.String() == "?":
		common.StartSearch(&ts.CopyState, true)
		ts.mu.Unlock()
		return nil

	case msg.String() == "n":
		common.RepeatSearch(term, &ts.CopyState, ts.CopyState.LastSearchBackward)
		ts.mu.Unlock()
		return nil

	case msg.String() == "N":
		common.RepeatSearch(term, &ts.CopyState, !ts.CopyState.LastSearchBackward)
		ts.mu.Unlock()
		return nil

	// Move left/right
	case msg.String() == "h":
		fallthrough
	case k.Code == tea.KeyLeft:
		ts.CopyState.CursorX--
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "l":
		fallthrough
	case k.Code == tea.KeyRight:
		ts.CopyState.CursorX++
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Move up/down
	case msg.String() == "k":
		fallthrough
	case k.Code == tea.KeyUp:
		ts.CopyState.CursorLine--
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "j":
		fallthrough
	case k.Code == tea.KeyDown:
		ts.CopyState.CursorLine++
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Word motions
	case msg.String() == "w":
		common.MoveWordForward(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "b":
		common.MoveWordBackward(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "e":
		common.MoveWordEnd(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Scroll up/down half page
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		fallthrough
	case k.Code == tea.KeyPgUp:
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		ts.CopyState.CursorLine -= delta
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+f"))):
		fallthrough
	case k.Code == tea.KeyPgDown:
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		ts.CopyState.CursorLine += delta
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Scroll to top/bottom
	case msg.String() == "g":
		ts.CopyState.CursorLine = 0
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "G":
		total := term.TotalLines()
		if total > 0 {
			ts.CopyState.CursorLine = total - 1
		}
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Move to top/middle/bottom of view
	case msg.String() == "H":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = start
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	case msg.String() == "M":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = start + (end-start)/2
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	case msg.String() == "L":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = end - 1
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	// Line start/end
	case msg.String() == "0":
		fallthrough
	case k.Code == tea.KeyHome:
		ts.CopyState.CursorX = 0
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "$":
		fallthrough
	case k.Code == tea.KeyEnd:
		ts.CopyState.CursorX = term.Width - 1
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil
	}

	ts.mu.Unlock()
	// Ignore other keys in copy mode
	return nil
}
