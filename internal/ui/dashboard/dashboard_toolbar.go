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
		{kind: toolbarCommands, label: "Commands"},
		{kind: toolbarSettings, label: "Settings"},
	}
}

func (m *Model) toolbarCommand(kind toolbarButtonKind) tea.Cmd {
	switch kind {
	case toolbarCommands:
		return func() tea.Msg { return messages.ShowCommandsPalette{} }
	case toolbarSettings:
		return func() tea.Msg { return messages.ShowSettingsDialog{} }
	default:
		return nil
	}
}

// renderToolbar renders the action buttons toolbar as a single row of buttons.
func (m *Model) renderToolbar() string {
	m.toolbarHits = m.toolbarHits[:0]

	buttonHeight := 1
	gap := 1
	items := m.toolbarItems()
	if len(items) == 0 {
		return ""
	}
	if m.toolbarIndex >= len(items) {
		m.toolbarIndex = len(items) - 1
	}

	activeStyle := lipgloss.NewStyle().
		Foreground(common.ColorForeground()).
		Bold(true)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())

	var row strings.Builder
	rowX := 0
	for i, item := range items {
		if i > 0 {
			row.WriteString(strings.Repeat(" ", gap))
			rowX += gap
		}
		label := "[" + item.label + "]"
		style := inactiveStyle
		if m.toolbarFocused && i == m.toolbarIndex {
			style = activeStyle
		}
		rendered := style.Render(label)
		width := lipgloss.Width(rendered)
		m.toolbarHits = append(m.toolbarHits, toolbarButton{
			kind: item.kind,
			region: common.HitRegion{
				X:      rowX,
				Y:      0,
				Width:  width,
				Height: buttonHeight,
			},
		})
		row.WriteString(rendered)
		rowX += width
	}

	return row.String()
}

// toolbarHeight returns the current toolbar height (always single row)
func (m *Model) toolbarHeight() int {
	if len(m.toolbarItems()) == 0 {
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
			// Mouse-triggered actions should not leave persistent toolbar focus
			// after opening/closing overlays.
			m.toolbarFocused = false
			m.toolbarIndex = i
			return m.toolbarCommand(hit.kind)
		}
	}
	return nil
}
