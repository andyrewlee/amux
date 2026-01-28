package inspector

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func actionZoneID(idx int) string {
	return fmt.Sprintf("inspector-action-%d", idx)
}

func toggleZoneID(id string) string {
	return "inspector-toggle-" + id
}

func reviewZoneID(id string) string {
	return "inspector-review-" + id
}

func conflictZoneID(id string) string {
	return "inspector-conflict-" + id
}

func prZoneID() string {
	return "inspector-pr"
}

func queueZoneID(id string) string {
	return "inspector-queue-" + id
}

func composerZoneID(id string) string {
	return "inspector-composer-" + id
}

func assigneeZoneID() string {
	return "inspector-assignee"
}

func labelZoneID(idx int) string {
	return fmt.Sprintf("inspector-label-%d", idx)
}

func breadcrumbZoneID() string {
	return "inspector-breadcrumb"
}

func gitActionZoneID(id string) string {
	return "inspector-git-" + id
}

func quickActionZoneID(id string) string {
	return "inspector-quick-" + id
}

func commentMeta(comment linear.Comment) string {
	author := "Unknown"
	if comment.User != nil && comment.User.Name != "" {
		author = comment.User.Name
	}
	when := ""
	if !comment.CreatedAt.IsZero() {
		when = " • " + relativeTime(comment.CreatedAt)
	}
	return author + when
}

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func padRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-lipgloss.Width(text))
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func actionRequiresAuth(id string) bool {
	switch id {
	case "start", "new_attempt", "move", "comment", "mark_done", "send_feedback":
		return true
	default:
		return false
	}
}

func extractTodos(entries []common.ActivityEntry) []string {
	seen := make(map[string]bool)
	var todos []string
	for _, entry := range entries {
		candidates := []string{entry.Summary}
		candidates = append(candidates, entry.Details...)
		for _, line := range candidates {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(strings.ToUpper(line), "TODO") || strings.Contains(line, "[ ]") || strings.HasPrefix(line, "- [ ]") {
				if !seen[line] {
					seen[line] = true
					todos = append(todos, line)
				}
			}
		}
	}
	return todos
}

func activityStatusIcon(status common.ActivityStatus) string {
	switch status {
	case common.StatusRunning:
		return common.Icons.Running
	case common.StatusPending:
		return common.Icons.Pending
	case common.StatusError:
		return common.Icons.Delete
	default:
		return common.Icons.Clean
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
