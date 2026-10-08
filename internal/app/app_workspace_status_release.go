package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// reservationReleaseResultMsg carries the off-loop probe+release outcome for
// the typed-confirm flow: count is the number of reservations actually
// deleted (0 with nil err means the orphan set was already gone).
type reservationReleaseResultMsg struct {
	count int
	err   error
}

// openReservationReleaseDialog swaps the workspace-status viewer for the
// typed-confirm gate. The viewer closes first — overlay arbitration forbids
// stacking a dialog over it — and the displayed count is bound into the
// dialog context as the expected confirmation, enforced at the consumer
// alongside the fresh orphan probe (the released set is still re-derived at
// confirm time, never trusted from display).
func (a *App) openReservationReleaseDialog(count int) {
	if count <= 0 {
		return
	}
	a.closeRunOutputDialog()
	want := strconv.Itoa(count)
	a.dialog = common.NewInputDialog(
		DialogReleasePortReservations,
		fmt.Sprintf("Release %d Port Reservations", count),
		"Type "+want+" to confirm",
	)
	a.dialog.SetInputValidate(func(s string) string {
		if strings.TrimSpace(s) != want {
			return "Type " + want + " to confirm the release"
		}
		return ""
	})
	a.dlg = dialogContext{portReleaseCount: count}
	a.presentDialog(a.dialog)
}

// dialogResultReleasePortReservations runs the release on confirm. Two
// gates: the typed value must equal the displayed count captured in the
// dialog's context (intent enforcement — an empty or wrong submission never
// reaches the registry), and the orphan set is RE-DERIVED inside the cmd,
// never trusted from display time (a workspace could have been deleted or
// its sessions recreated since the viewer opened). Re-derivation is
// fail-closed: a stale probe under-releases, never over-.
func dialogResultReleasePortReservations(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if !result.Confirmed || a.workspaceService == nil {
		return nil
	}
	if dlg.portReleaseCount <= 0 ||
		strings.TrimSpace(result.Value) != strconv.Itoa(dlg.portReleaseCount) {
		return nil
	}
	svc := a.workspaceService
	knownIDs := a.collectPortReservationOwnerIDs()
	opts := a.tmuxOptions
	return func() tea.Msg {
		ids := a.reclaimablePortReservationIDs(svc, knownIDs, opts)
		if len(ids) == 0 {
			return reservationReleaseResultMsg{}
		}
		released, err := svc.ReleasePortReservations(ids)
		if err != nil {
			return reservationReleaseResultMsg{err: err}
		}
		return reservationReleaseResultMsg{count: len(released)}
	}
}

// handleReservationReleaseResult reports the release outcome. A zero count
// is honest — the orphans resolved themselves (or a fresh probe found live
// sessions) between display and confirm, so nothing was deleted.
func (a *App) handleReservationReleaseResult(msg reservationReleaseResultMsg) tea.Cmd {
	if msg.err != nil {
		logging.Error("port reservation release failed: %v", msg.err)
		return a.toast.ShowError("Reservation release failed: " + msg.err.Error())
	}
	if msg.count == 0 {
		return a.toast.ShowInfo("No orphaned reservations to release")
	}
	return a.toast.ShowInfo(fmt.Sprintf("Released %d orphaned port reservation(s)", msg.count))
}
