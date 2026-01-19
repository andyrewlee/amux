package center

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleCopyModeKey handles keys while in copy mode (scroll navigation)
func (m *Model) handleCopyModeKey(tab *Tab, msg tea.KeyPressMsg) tea.Cmd {
	var copyText string
	var didCopy bool

	tab.mu.Lock()
	term := tab.Terminal
	k := msg.Key()
	if term == nil {
		// Allow exiting copy mode even when no terminal is available.
		if k.Code == tea.KeyEsc || k.Code == tea.KeyEscape || msg.String() == "q" {
			tab.CopyMode = false
			tab.CopyState = common.CopyState{}
		}
		tab.mu.Unlock()
		return nil
	}
	if tab.CopyState.SearchActive {
		switch {
		case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
			common.CancelSearch(&tab.CopyState)
		case k.Code == tea.KeyEnter:
			common.ExecuteSearch(term, &tab.CopyState)
		case k.Code == tea.KeyBackspace:
			common.BackspaceSearchQuery(&tab.CopyState)
		default:
			if k.Text != "" && (k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper)) == 0 {
				common.AppendSearchQuery(&tab.CopyState, k.Text)
			}
		}
		tab.mu.Unlock()
		return nil
	}

	switch {
	// Exit copy mode
	case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
		fallthrough
	case msg.String() == "q":
		tab.CopyMode = false
		tab.CopyState = common.CopyState{}
		term.ClearSelection()
		term.ScrollViewToBottom()
		tab.mu.Unlock()
		return nil

	// Copy selection
	case k.Code == tea.KeyEnter || msg.String() == "y":
		copyText = common.CopySelectionText(term, &tab.CopyState)
		if copyText != "" {
			tab.CopyMode = false
			tab.CopyState = common.CopyState{}
			term.ClearSelection()
			term.ScrollViewToBottom()
			didCopy = true
		}
		tab.mu.Unlock()
		if didCopy {
			if err := common.CopyToClipboard(copyText); err != nil {
				logging.Error("Failed to copy to clipboard: %v", err)
			} else {
				logging.Info("Copied %d chars to clipboard", len(copyText))
			}
		}
		return nil

	// Toggle selection
	case msg.String() == " " || msg.String() == "v":
		common.ToggleCopySelection(&tab.CopyState)
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Toggle rectangle selection
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+v"))):
		common.ToggleRectangle(&tab.CopyState)
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Search
	case msg.String() == "/":
		common.StartSearch(&tab.CopyState, false)
		tab.mu.Unlock()
		return nil

	case msg.String() == "?":
		common.StartSearch(&tab.CopyState, true)
		tab.mu.Unlock()
		return nil

	case msg.String() == "n":
		common.RepeatSearch(term, &tab.CopyState, tab.CopyState.LastSearchBackward)
		tab.mu.Unlock()
		return nil

	case msg.String() == "N":
		common.RepeatSearch(term, &tab.CopyState, !tab.CopyState.LastSearchBackward)
		tab.mu.Unlock()
		return nil

	// Move left/right
	case msg.String() == "h":
		fallthrough
	case k.Code == tea.KeyLeft:
		tab.CopyState.CursorX--
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "l":
		fallthrough
	case k.Code == tea.KeyRight:
		tab.CopyState.CursorX++
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Move up/down
	case msg.String() == "k":
		fallthrough
	case k.Code == tea.KeyUp:
		tab.CopyState.CursorLine--
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "j":
		fallthrough
	case k.Code == tea.KeyDown:
		tab.CopyState.CursorLine++
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Word motions
	case msg.String() == "w":
		common.MoveWordForward(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "b":
		common.MoveWordBackward(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "e":
		common.MoveWordEnd(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Scroll up/down half page
	case k.Code == tea.KeyPgUp:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		tab.CopyState.CursorLine -= delta
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case k.Code == tea.KeyPgDown:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+f"))):
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		tab.CopyState.CursorLine += delta
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Scroll to top/bottom
	case msg.String() == "g":
		tab.CopyState.CursorLine = 0
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "G":
		total := term.TotalLines()
		if total > 0 {
			tab.CopyState.CursorLine = total - 1
		}
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	// Move to top/middle/bottom of view
	case msg.String() == "H":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			tab.CopyState.CursorLine = start
			common.SyncCopyState(term, &tab.CopyState)
		}
		tab.mu.Unlock()
		return nil

	case msg.String() == "M":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			tab.CopyState.CursorLine = start + (end-start)/2
			common.SyncCopyState(term, &tab.CopyState)
		}
		tab.mu.Unlock()
		return nil

	case msg.String() == "L":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			tab.CopyState.CursorLine = end - 1
			common.SyncCopyState(term, &tab.CopyState)
		}
		tab.mu.Unlock()
		return nil

	// Line start/end
	case msg.String() == "0":
		fallthrough
	case k.Code == tea.KeyHome:
		tab.CopyState.CursorX = 0
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil

	case msg.String() == "$":
		fallthrough
	case k.Code == tea.KeyEnd:
		tab.CopyState.CursorX = term.Width - 1
		common.SyncCopyState(term, &tab.CopyState)
		tab.mu.Unlock()
		return nil
	}

	tab.mu.Unlock()
	// Ignore other keys in copy mode (don't forward to PTY)
	return nil
}
