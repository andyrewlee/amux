package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// updateFlowState models the upgrade-prompt flow over App.updateAvailable
// (*update.CheckResult; nil = none-or-dismissed) and App.upgradeRunning.
// The versions and release notes are extended state; this is the control
// shape: an upgrade may only start from an available result, and a failed
// upgrade returns to available (retryable) rather than clearing the
// prompt.
type updateFlowState uint8

const (
	// updateFlowNone: no update known (updateAvailable == nil).
	updateFlowNone updateFlowState = iota
	// updateFlowAvailable: a check found an update (updateAvailable set).
	updateFlowAvailable
	// updateFlowUpgrading: handleTriggerUpgrade dispatched the upgrade cmd
	// (upgradeRunning latched; the re-entry guard).
	updateFlowUpgrading
)

func (s updateFlowState) String() string {
	switch s {
	case updateFlowAvailable:
		return "available"
	case updateFlowUpgrading:
		return "upgrading"
	default:
		return "none"
	}
}

// updateFlowEvent is the update flow's alphabet.
type updateFlowEvent uint8

const (
	// updateEvtCheckFound: an UpdateCheckComplete found an update.
	updateEvtCheckFound updateFlowEvent = iota
	// updateEvtCheckNone: an UpdateCheckComplete found nothing. Declared as
	// a self-loop on available — a recheck that finds nothing does NOT
	// clear a previously found update (the handler returns before touching
	// updateAvailable). This documents the quirk rather than papering over
	// it: a stale prompt can survive a later no-update check.
	updateEvtCheckNone
	// updateEvtTrigger: the user (or the settings dialog) triggered the
	// upgrade.
	updateEvtTrigger
	// updateEvtUpgradeSucceeded: UpgradeComplete without error — the only
	// event that clears a found update.
	updateEvtUpgradeSucceeded
	// updateEvtUpgradeFailed: UpgradeComplete with an error — the result is
	// retained for retry.
	updateEvtUpgradeFailed
	// updateEvtPanicReset: resetAdvisoryLatchesAfterPanic cleared the
	// upgradeRunning latch (the producer cmd was discarded by the panic —
	// without the clear the path dead-ends for the session).
	updateEvtPanicReset
)

func (e updateFlowEvent) String() string {
	switch e {
	case updateEvtCheckFound:
		return "check-found"
	case updateEvtCheckNone:
		return "check-none"
	case updateEvtTrigger:
		return "trigger"
	case updateEvtUpgradeSucceeded:
		return "upgrade-succeeded"
	case updateEvtUpgradeFailed:
		return "upgrade-failed"
	default:
		return "panic-reset"
	}
}

// updateFlowMachine is the update-prompt table.
var updateFlowMachine = fsm.MustCompile(fsm.Machine[updateFlowState, updateFlowEvent]{
	Name:    "update-flow",
	Initial: updateFlowNone,
	Steps: []fsm.Step[updateFlowState, updateFlowEvent]{
		{From: updateFlowNone, Event: updateEvtCheckFound, To: updateFlowAvailable},
		{From: updateFlowAvailable, Event: updateEvtCheckNone, To: updateFlowAvailable},
		{From: updateFlowAvailable, Event: updateEvtTrigger, To: updateFlowUpgrading},
		{From: updateFlowUpgrading, Event: updateEvtUpgradeSucceeded, To: updateFlowNone},
		{From: updateFlowUpgrading, Event: updateEvtUpgradeFailed, To: updateFlowAvailable},
		{From: updateFlowUpgrading, Event: updateEvtPanicReset, To: updateFlowAvailable},
	},
})

// updateFlowCheck formally verifies the update flow: an upgrade starts only
// from an available result (the nil/upgradeRunning guards in
// handleTriggerUpgrade), a successful upgrade is the only event that
// returns to none, a failed upgrade stays available (retryable), and a
// panic-cleared upgrade latch also returns to available rather than
// dead-ending.
func updateFlowCheck() error {
	return updateFlowMachine.Check(fsm.Checker[updateFlowState, updateFlowEvent]{
		Alphabet: []updateFlowEvent{
			updateEvtCheckFound, updateEvtCheckNone, updateEvtTrigger,
			updateEvtUpgradeSucceeded, updateEvtUpgradeFailed, updateEvtPanicReset,
		},
		AllDeclaredReachable: true,
		Edge: func(from updateFlowState, event updateFlowEvent, to updateFlowState) error {
			if event == updateEvtTrigger && from != updateFlowAvailable {
				return errors.New("an upgrade may only start from an available result")
			}
			if to == updateFlowNone && event != updateEvtUpgradeSucceeded {
				return errors.New("only a successful upgrade clears the prompt")
			}
			if event == updateEvtUpgradeFailed && to != updateFlowAvailable {
				return errors.New("a failed upgrade must remain available for retry")
			}
			return nil
		},
	})
}
