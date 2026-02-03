package dashboard

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type toolbarItem struct {
	kind  toolbarButtonKind
	label string
}

func (m *Model) toolbarItems() []toolbarItem {
	return []toolbarItem{
		{kind: toolbarHelp, label: "?H"},
		{kind: toolbarSettings, label: "⚙S"},
	}
}

func (m *Model) toolbarCommand(kind toolbarButtonKind) tea.Cmd {
	switch kind {
	case toolbarHelp:
		return func() tea.Msg { return messages.ToggleHelp{} }
	case toolbarSettings:
		return func() tea.Msg { return messages.ShowSettingsDialog{} }
	default:
		return nil
	}
}

// renderToolbar renders the action buttons toolbar
func (m *Model) renderToolbar() string {
	m.toolbarHits = m.toolbarHits[:0]

	buttonHeight := 1
	gap := 1
	columns := 3
	items := m.toolbarItems()
	visibleItems := m.toolbarVisibleItems(items)
	if len(visibleItems) == 0 {
		return ""
	}
	if m.toolbarIndex >= len(visibleItems) {
		m.toolbarIndex = len(visibleItems) - 1
	}

	activeStyle := lipgloss.NewStyle().
		Foreground(common.ColorForeground).
		Bold(true)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted)

	var rows []string
	for rowStart := 0; rowStart < len(visibleItems); rowStart += columns {
		var row strings.Builder
		rowX := 0
		rowIndex := rowStart / columns
		for col := 0; col < columns && rowStart+col < len(visibleItems); col++ {
			if col > 0 {
				row.WriteString(strings.Repeat(" ", gap))
				rowX += gap
			}
			itemIndex := rowStart + col
			item := visibleItems[itemIndex]
			label := "[" + item.label + "]"
			style := inactiveStyle
			if m.toolbarFocused && itemIndex == m.toolbarIndex {
				style = activeStyle
			}
			rendered := style.Render(label)
			width := lipgloss.Width(rendered)
			m.toolbarHits = append(m.toolbarHits, toolbarButton{
				kind: item.kind,
				region: common.HitRegion{
					X:      rowX,
					Y:      rowIndex,
					Width:  width,
					Height: buttonHeight,
				},
			})
			row.WriteString(rendered)
			rowX += width
		}
		rows = append(rows, row.String())
	}

	return strings.Join(rows, "\n")
}

func (m *Model) toolbarVisibleItems(items []toolbarItem) []toolbarItem {
	return items
}

// toolbarHeight returns the current toolbar height (always single row)
func (m *Model) toolbarHeight() int {
	visibleItems := m.toolbarVisibleItems(m.toolbarItems())
	if len(visibleItems) == 0 {
		return 0
	}
	return 1
}

// handleToolbarClick checks if a click is on a toolbar button and returns the appropriate command
func (m *Model) handleToolbarClick(screenX, screenY int) tea.Cmd {
	// Convert screen coordinates to content coordinates
	borderTop := 1
	borderLeft := 1
	paddingLeft := 0

	contentX := screenX - borderLeft - paddingLeft
	contentY := screenY - borderTop

	toolbarHeight := m.toolbarHeight()

	// Check if click is within the toolbar area
	if contentY < m.toolbarY || contentY >= m.toolbarY+toolbarHeight {
		return nil
	}

	// Calculate Y relative to toolbar start
	localY := contentY - m.toolbarY

	// Check toolbar button hits
	for i, hit := range m.toolbarHits {
		if hit.region.Contains(contentX, localY) {
			m.toolbarFocused = true
			m.toolbarIndex = i
			return m.toolbarCommand(hit.kind)
		}
	}
	return nil
}
