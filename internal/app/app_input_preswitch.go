package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handlePreSwitchInput runs the overlay/dialog guards that may consume a message
// before the main routing switch. It returns the resulting command and true when
// the message was consumed (the caller returns immediately).
func (a *App) handlePreSwitchInput(msg tea.Msg, cmds *[]tea.Cmd) (tea.Cmd, bool) {
	if perf.Enabled() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			a.markInput()
		}
	}

	if handled, cmd := a.handleDialogResultMsg(msg); handled {
		return cmd, true
	}
	if a.handleErrorOverlayDismiss(msg) {
		return nil, true
	}

	// Handle toast updates (does not consume the message).
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		*cmds = append(*cmds, cmd)
	}

	// Bespoke overlays consume in overlayChain order — the first live overlay
	// eats the message. Order is load-bearing (see app_overlays.go).
	for _, slot := range a.overlayChain() {
		if slot(msg, cmds) {
			return common.SafeBatch(*cmds...), true
		}
	}
	return nil, false
}
