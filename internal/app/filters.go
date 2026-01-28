package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) showLabelFilterDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	labels := map[string]bool{}
	for _, issue := range a.boardIssues {
		for _, label := range issue.Labels {
			if label.Name != "" {
				labels[label.Name] = true
			}
		}
	}
	options := []string{"All labels"}
	values := []string{""}
	keys := make([]string, 0, len(labels))
	for label := range labels {
		keys = append(keys, label)
	}
	sort.Strings(keys)
	for _, label := range keys {
		options = append(options, label)
		values = append(values, label)
	}
	a.labelFilterValues = values
	a.dialog = common.NewSelectDialog("board-label-filter", "Label Filter", "Select label:", options)

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) showRecentFilterDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	options := []string{"All updates", "Last 1 day", "Last 7 days", "Last 30 days"}
	values := []int{0, 1, 7, 30}
	a.recentFilterValues = values
	a.dialog = common.NewSelectDialog("board-recent-filter", "Updated Recently", "Select window:", options)

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func normalizeLabel(label string) string {
	return strings.TrimSpace(strings.ToLower(label))
}

func (a *App) applyLabelFilter(label string) tea.Cmd {
	normalized := normalizeLabel(label)
	if normalizeLabel(a.board.Filters.Label) == normalized {
		normalized = ""
	}
	a.board.Filters.Label = normalized
	a.updateBoard(a.boardIssues)
	return func() tea.Msg { return messages.BoardFilterChanged{} }
}
