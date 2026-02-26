package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) renderPrefixPalette() string {
	if !a.prefixActive || a.width <= 0 || a.height <= 0 {
		return ""
	}

	panelWidth := a.width
	contentWidth := panelWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	sequence := "C-Space"
	if len(a.prefixSequence) > 0 {
		sequence += " " + strings.Join(a.prefixSequence, " ")
	}
	sections := a.prefixPaletteSections()
	totalChoices := 0
	for _, section := range sections {
		totalChoices += len(section.Choices)
	}

	headerLeft := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary()).
		Render(sequence)
	caret := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorMuted()).
		Render("  >")
	headerLeft += caret
	headerRight := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Render(fmt.Sprintf("%d choices", totalChoices))
	header := joinWithRightEdge(headerLeft, headerRight, contentWidth)

	lines := []string{header}
	for _, section := range sections {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(common.ColorMuted()).Render(section.Title))
		if len(section.Choices) == 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(common.ColorWarning()).Render("No matching command"))
			continue
		}
		lines = append(lines, a.renderChoiceColumns(section.Choices, contentWidth)...)
	}

	footer := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Render("Esc cancel | Backspace undo | C-Space reset | C-Space C-Space literal")

	maxLines := a.height - 3
	if maxLines < 2 {
		maxLines = 2
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = lipgloss.NewStyle().Foreground(common.ColorMuted()).Render("...")
	}
	body := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(panelWidth).
		Border(lipgloss.Border{Top: "─"}, true, false, false, false).
		BorderForeground(common.ColorBorder()).
		Padding(0, 1).
		Background(common.ColorSurface0()).
		Foreground(common.ColorForeground()).
		Render(body + "\n" + footer)
}

type prefixPaletteChoice struct {
	Key  string
	Desc string
}

type prefixPaletteSection struct {
	Title   string
	Choices []prefixPaletteChoice
}

func (a *App) prefixPaletteSections() []prefixPaletteSection {
	if len(a.prefixSequence) == 0 {
		return []prefixPaletteSection{
			{
				Title: "Navigation",
				Choices: []prefixPaletteChoice{
					{Key: "h", Desc: "focus left"},
					{Key: "j", Desc: "focus down"},
					{Key: "k", Desc: "focus up"},
					{Key: "l", Desc: "focus right"},
				},
			},
			{
				Title: "General",
				Choices: []prefixPaletteChoice{
					{Key: "?", Desc: "toggle help"},
					{Key: "q", Desc: "quit"},
					{Key: "K", Desc: "cleanup tmux"},
				},
			},
			{
				Title: "Tabs",
				Choices: []prefixPaletteChoice{
					{Key: "t", Desc: "tab commands"},
					{Key: "1-9", Desc: "jump tab"},
				},
			},
		}
	}
	if len(a.prefixSequence) == 1 && a.prefixSequence[0] == "t" {
		return []prefixPaletteSection{
			{
				Title: "Tabs",
				Choices: []prefixPaletteChoice{
					{Key: "a", Desc: "new agent tab"},
					{Key: "t", Desc: "new terminal tab"},
					{Key: "n", Desc: "next tab"},
					{Key: "p", Desc: "prev tab"},
					{Key: "x", Desc: "close tab"},
					{Key: "d", Desc: "detach tab"},
					{Key: "r", Desc: "reattach tab"},
					{Key: "s", Desc: "restart tab"},
				},
			},
		}
	}
	return []prefixPaletteSection{
		{
			Title:   "No Match",
			Choices: nil,
		},
	}
}

func joinWithRightEdge(left, right string, width int) string {
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 2 {
		space = 2
	}
	return left + strings.Repeat(" ", space) + right
}

func (a *App) renderChoiceColumns(choices []prefixPaletteChoice, contentWidth int) []string {
	if len(choices) == 0 {
		return nil
	}
	colCount := contentWidth / 30
	if colCount < 1 {
		colCount = 1
	}
	if colCount > len(choices) {
		colCount = len(choices)
	}
	for colCount > 1 {
		gutterWidth := (colCount - 1) * 2
		colWidth := (contentWidth - gutterWidth) / colCount
		if colWidth >= 20 {
			break
		}
		colCount--
	}

	columnSep := lipgloss.NewStyle().Foreground(common.ColorBorder()).Render("│")
	gutterWidth := (colCount - 1) * 3
	colWidth := (contentWidth - gutterWidth) / colCount
	if colWidth < 12 {
		colWidth = 12
	}

	keyWidth := 3
	for _, choice := range choices {
		w := lipgloss.Width(a.prefixKeyLabel(choice.Key))
		if w > keyWidth {
			keyWidth = w
		}
	}
	if keyWidth > 14 {
		keyWidth = 14
	}

	rows := (len(choices) + colCount - 1) / colCount
	lines := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		rowParts := make([]string, 0, colCount)
		for c := 0; c < colCount; c++ {
			idx := c*rows + r
			if idx >= len(choices) {
				rowParts = append(rowParts, strings.Repeat(" ", colWidth))
				continue
			}
			rowParts = append(rowParts, a.renderChoiceCell(choices[idx], keyWidth, colWidth))
		}
		lines = append(lines, strings.Join(rowParts, " "+columnSep+" "))
	}
	return lines
}

func (a *App) renderChoiceCell(choice prefixPaletteChoice, keyWidth, colWidth int) string {
	key := a.renderChoiceKey(choice.Key, keyWidth)
	sep := lipgloss.NewStyle().Foreground(common.ColorMuted()).Render(" -> ")
	descWidth := colWidth - keyWidth - lipgloss.Width(sep)
	if descWidth < 4 {
		descWidth = 4
	}
	desc := a.styles.HelpDesc.Render(ansi.Truncate(choice.Desc, descWidth, ""))
	cell := key + sep + desc
	if w := lipgloss.Width(cell); w < colWidth {
		cell += strings.Repeat(" ", colWidth-w)
	}
	return cell
}

func (a *App) prefixKeyLabel(actionKey string) string {
	if len(a.prefixSequence) == 0 {
		return actionKey
	}
	return strings.Join(a.prefixSequence, " ") + " " + actionKey
}

func (a *App) renderChoiceKey(actionKey string, keyWidth int) string {
	if len(a.prefixSequence) == 0 {
		return a.styles.HelpKey.Width(keyWidth).Render(actionKey)
	}
	prefix := strings.Join(a.prefixSequence, " ")
	prefixStyle := lipgloss.NewStyle().Foreground(common.ColorMuted())
	actionStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary()).Bold(true)
	key := prefixStyle.Render(prefix) + " " + actionStyle.Render(actionKey)
	if w := lipgloss.Width(key); w < keyWidth {
		key += strings.Repeat(" ", keyWidth-w)
	}
	if w := lipgloss.Width(key); w > keyWidth {
		key = ansi.Truncate(key, keyWidth, "")
	}
	return key
}
