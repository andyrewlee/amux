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

const prefixPaletteColumnGutterWidth = 3 // " │ "

func (a *App) prefixPaletteSections() []prefixPaletteSection {
	if len(a.prefixSequence) == 0 {
		return a.rootPrefixPaletteSections()
	}

	choices := a.nextPrefixPaletteChoices()
	if len(choices) > 0 {
		return []prefixPaletteSection{
			{
				Title:   prefixPaletteGroupTitle(a.prefixSequence[0]),
				Choices: choices,
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

func (a *App) rootPrefixPaletteSections() []prefixPaletteSection {
	choiceByKey := map[string]prefixPaletteChoice{}
	groupByKey := map[string]string{}
	order := make([]string, 0, len(a.prefixCommands()))

	for _, cmd := range a.prefixCommands() {
		if len(cmd.Sequence) == 0 {
			continue
		}
		key := cmd.Sequence[0]
		group := prefixPaletteGroupTitle(key)
		if _, ok := choiceByKey[key]; !ok {
			desc := cmd.Desc
			if len(cmd.Sequence) > 1 {
				desc = prefixPaletteGroupDesc(key)
			}
			choiceByKey[key] = prefixPaletteChoice{Key: key, Desc: desc}
			groupByKey[key] = group
			order = append(order, key)
		}
		// Prefer concrete single-step descriptions when available.
		if len(cmd.Sequence) == 1 {
			choiceByKey[key] = prefixPaletteChoice{Key: key, Desc: cmd.Desc}
		}
	}

	grouped := map[string][]prefixPaletteChoice{}
	for _, key := range order {
		grouped[groupByKey[key]] = append(grouped[groupByKey[key]], choiceByKey[key])
	}
	// Numeric tab jumping is a root-level special case in handlePrefixCommand.
	grouped["Tabs"] = append(grouped["Tabs"], prefixPaletteChoice{Key: "1-9", Desc: "jump tab"})

	titles := []string{"Navigation", "General", "Tabs"}
	sections := make([]prefixPaletteSection, 0, len(titles))
	for _, title := range titles {
		if len(grouped[title]) == 0 {
			continue
		}
		sections = append(sections, prefixPaletteSection{
			Title:   title,
			Choices: grouped[title],
		})
	}
	return sections
}

func (a *App) nextPrefixPaletteChoices() []prefixPaletteChoice {
	matches := a.matchingPrefixCommands(a.prefixSequence)
	if len(matches) == 0 {
		return nil
	}

	seqLen := len(a.prefixSequence)
	order := make([]string, 0, len(matches))
	descByKey := map[string]string{}
	hasLeaf := map[string]bool{}

	for _, cmd := range matches {
		if len(cmd.Sequence) <= seqLen {
			continue
		}
		key := cmd.Sequence[seqLen]
		if _, ok := descByKey[key]; !ok {
			descByKey[key] = prefixPaletteGroupDesc(key)
			order = append(order, key)
		}
		if len(cmd.Sequence) == seqLen+1 {
			descByKey[key] = cmd.Desc
			hasLeaf[key] = true
			continue
		}
		if !hasLeaf[key] {
			descByKey[key] = prefixPaletteGroupDesc(key)
		}
	}

	choices := make([]prefixPaletteChoice, 0, len(order))
	for _, key := range order {
		choices = append(choices, prefixPaletteChoice{Key: key, Desc: descByKey[key]})
	}
	return choices
}

func prefixPaletteGroupTitle(token string) string {
	switch token {
	case "h", "j", "k", "l":
		return "Navigation"
	case "t":
		return "Tabs"
	default:
		return "General"
	}
}

func prefixPaletteGroupDesc(token string) string {
	switch token {
	case "t":
		return "tab commands"
	default:
		return "commands"
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
		gutterWidth := (colCount - 1) * prefixPaletteColumnGutterWidth
		colWidth := (contentWidth - gutterWidth) / colCount
		if colWidth >= 20 {
			break
		}
		colCount--
	}

	columnSep := lipgloss.NewStyle().Foreground(common.ColorBorder()).Render("│")
	gutterWidth := (colCount - 1) * prefixPaletteColumnGutterWidth
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
