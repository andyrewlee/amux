package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleShowEventLog opens the session notification recall surface — the
// bounded ring of every toast the app showed, newest-first. It reuses the
// runOutput dialog slot's static-viewer shape (the i-status precedent): no
// session, no lifecycle, no refresh tick; y copy and / search come free.
func (a *App) handleShowEventLog() tea.Cmd {
	events := a.toast.Events()
	if len(events) == 0 {
		return a.toast.ShowInfo("No notifications yet this session")
	}
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = nil
		a.overlays.runOutputSession = ""
		a.overlays.runOutputAttachable = false
		a.overlays.runOutputReleaseCount = 0
		a.overlays.runOutputUntrustable = false
		a.overlays.runOutputLifecycle = false
		a.overlays.runOutputToken++
		a.overlays.runOutput = common.NewOutputDialog("Notifications", formatSessionEvents(events))
		a.overlays.runOutput.SetSearchable(true)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	return nil
}

// formatSessionEvents renders ring entries as `HH:MM:SS severity  text`
// rows — the snapshot is already newest-first, matching the overlay's
// read-down order.
func formatSessionEvents(events []common.SessionEvent) string {
	var b strings.Builder
	for i, e := range events {
		if i > 0 {
			b.WriteString("\n")
		}
		text := common.SanitizeDisplayText(e.Text, 512)
		text = strings.ReplaceAll(text, "\n", " ")
		fmt.Fprintf(&b, "%s  %-7s %s", e.Time.Format("15:04:05"), sessionEventLabel(e.Type), text)
	}
	return b.String()
}

// sessionEventLabel names each toast severity for the recall rows — plain
// words rather than the live toast's color/icon styling.
func sessionEventLabel(t common.ToastType) string {
	switch t {
	case common.ToastError:
		return "error"
	case common.ToastWarning:
		return "warn"
	case common.ToastSuccess:
		return "ok"
	default:
		return "info"
	}
}
