package sidebar

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle filter input when in filter mode
	if m.filterMode {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.filterMode = false
				m.filterQuery = ""
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.rebuildDisplayList()
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				m.filterMode = false
				m.filterInput.Blur()
				return m, nil
			default:
				newInput, cmd := m.filterInput.Update(msg)
				m.filterInput = newInput
				m.filterQuery = m.filterInput.Value()
				m.rebuildDisplayList()
				return m, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		delta := common.ScrollDeltaForHeight(m.visibleHeight(), 10) // ~10% of visible
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-delta)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
			return m, nil
		}

	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			idx, ok := m.rowIndexAt(msg.Y)
			if !ok {
				return m, nil
			}
			m.cursor = idx
			return m, m.openCurrentItem()
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", "space", "o"))):
			cmds = append(cmds, m.openCurrentItem())
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			cmds = append(cmds, m.refreshStatus())
		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			// Enter filter mode
			m.filterMode = true
			m.filterInput.Focus()
			return m, m.filterInput.Focus()
		}
	}

	return m, common.SafeBatch(cmds...)
}

// openCurrentItem opens the diff for the currently selected item.
func (m *Model) openCurrentItem() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.displayItems) {
		return nil
	}

	item := m.displayItems[m.cursor]
	if item.isHeader || item.change == nil {
		return nil
	}

	change := item.change
	mode := item.mode
	ws := m.workspace

	return func() tea.Msg {
		return messages.OpenDiff{
			Change:    change,
			Mode:      mode,
			Workspace: ws,
		}
	}
}

func (m *Model) rowIndexAt(screenY int) (int, bool) {
	if m.gitStatus == nil || m.gitStatus.Clean {
		return -1, false
	}
	if len(m.displayItems) == 0 {
		return -1, false
	}
	header := m.listHeaderLines()
	help := m.helpLineCount()
	contentHeight := m.height - help
	if screenY < header || screenY >= contentHeight {
		return -1, false
	}
	rowY := screenY - header
	if rowY < 0 || rowY >= m.visibleHeight() {
		return -1, false
	}
	index := m.scrollOffset + rowY
	if index < 0 || index >= len(m.displayItems) {
		return -1, false
	}
	return index, true
}

// moveCursor moves the cursor, skipping section headers.
func (m *Model) moveCursor(delta int) {
	if len(m.displayItems) == 0 {
		return
	}

	newCursor := m.cursor + delta

	// Skip headers when moving
	for newCursor >= 0 && newCursor < len(m.displayItems) && m.displayItems[newCursor].isHeader {
		if delta > 0 {
			newCursor++
		} else {
			newCursor--
		}
	}

	// Clamp to valid range
	if newCursor < 0 {
		newCursor = 0
		// Find first non-header
		for newCursor < len(m.displayItems) && m.displayItems[newCursor].isHeader {
			newCursor++
		}
	}
	if newCursor >= len(m.displayItems) {
		newCursor = len(m.displayItems) - 1
		// Find last non-header
		for newCursor >= 0 && m.displayItems[newCursor].isHeader {
			newCursor--
		}
	}

	if newCursor >= 0 && newCursor < len(m.displayItems) {
		m.cursor = newCursor
	}
}

// refreshStatus refreshes the git status.
func (m *Model) refreshStatus() tea.Cmd {
	if m.workspace == nil {
		return nil
	}

	root := m.workspace.Root
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		return messages.GitStatusResult{Root: root, Status: status, Err: err}
	}
}
