package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// settingsObligationState models one settings section's save obligation
// (the UI section: settingsUIPersistPending + settingsThemeDirty; the
// assistants section: settingsAssistantsPersistPending). The load-bearing
// contract the code defends by hand — "once a save is owed it stays owed
// until that section's write succeeds" — becomes a checked edge property:
// a failed write never discharges the obligation, and only a successful
// write reaches clean. The theme IDs, config values, and the
// settingsSession preview generation are extended state.
type settingsObligationState uint8

const (
	// settingsObligationClean: nothing owed for this section.
	settingsObligationClean settingsObligationState = iota
	// settingsObligationDirty: this dialog confirmed changes (or the live
	// theme diverges from the persisted one) — a write is owed now.
	settingsObligationDirty
	// settingsObligationRetry: a write was attempted and failed — the
	// obligation persists for the NEXT confirmation, even one that changed
	// nothing in this section.
	settingsObligationRetry
)

func (s settingsObligationState) String() string {
	switch s {
	case settingsObligationDirty:
		return "dirty"
	case settingsObligationRetry:
		return "retry-owed"
	default:
		return "clean"
	}
}

// settingsObligationEvent is the obligation machine's alphabet.
type settingsObligationEvent uint8

const (
	// settingsEvtConfirmChanged: a confirmation that changed this section —
	// the obligation is recorded BEFORE the write is attempted.
	settingsEvtConfirmChanged settingsObligationEvent = iota
	// settingsEvtConfirmUnchanged: a confirmation that changed nothing here
	// (still attempts the write when an obligation is owed).
	settingsEvtConfirmUnchanged
	// settingsEvtSaveSucceeded: the section write succeeded — the only
	// event that discharges the obligation.
	settingsEvtSaveSucceeded
	// settingsEvtSaveFailed: the section write failed — the obligation is
	// retained (persistSettings*IfDirty leaves the pending flag set).
	settingsEvtSaveFailed
)

func (e settingsObligationEvent) String() string {
	switch e {
	case settingsEvtConfirmChanged:
		return "confirm-changed"
	case settingsEvtConfirmUnchanged:
		return "confirm-unchanged"
	case settingsEvtSaveSucceeded:
		return "save-succeeded"
	default:
		return "save-failed"
	}
}

// settingsObligationMachine is the per-section obligation table, shared by
// the UI section and the assistants section (one failed section must not
// skip the other's attempt — they run two independent copies of this
// machine).
var settingsObligationMachine = fsm.MustCompile(fsm.Machine[settingsObligationState, settingsObligationEvent]{
	Name:    "settings-save-obligation",
	Initial: settingsObligationClean,
	Steps: []fsm.Step[settingsObligationState, settingsObligationEvent]{
		{From: settingsObligationClean, Event: settingsEvtConfirmChanged, To: settingsObligationDirty},
		{From: settingsObligationClean, Event: settingsEvtConfirmUnchanged, To: settingsObligationClean},
		{From: settingsObligationDirty, Event: settingsEvtConfirmChanged, To: settingsObligationDirty},
		{From: settingsObligationDirty, Event: settingsEvtConfirmUnchanged, To: settingsObligationDirty},
		{From: settingsObligationDirty, Event: settingsEvtSaveSucceeded, To: settingsObligationClean},
		{From: settingsObligationDirty, Event: settingsEvtSaveFailed, To: settingsObligationRetry},
		// From retry, EVERY confirmation retries the write (pending ||
		// changed), so confirm-unchanged still routes through dirty (the
		// attempt in flight) rather than skipping the write.
		{From: settingsObligationRetry, Event: settingsEvtConfirmChanged, To: settingsObligationDirty},
		{From: settingsObligationRetry, Event: settingsEvtConfirmUnchanged, To: settingsObligationDirty},
		{From: settingsObligationRetry, Event: settingsEvtSaveSucceeded, To: settingsObligationClean},
		{From: settingsObligationRetry, Event: settingsEvtSaveFailed, To: settingsObligationRetry},
	},
})

// settingsObligationCheck formally verifies the obligation contract: only
// a successful write reaches clean (a failure stays owed — no event
// discharges the debt except the write itself), a retrying section always
// re-attempts on any confirmation (no unchanged-confirm can skip the
// write), and every failed write lands back on owed.
func settingsObligationCheck() error {
	return settingsObligationMachine.Check(fsm.Checker[settingsObligationState, settingsObligationEvent]{
		Alphabet: []settingsObligationEvent{
			settingsEvtConfirmChanged, settingsEvtConfirmUnchanged,
			settingsEvtSaveSucceeded, settingsEvtSaveFailed,
		},
		AllDeclaredReachable: true,
		Edge: func(_ settingsObligationState, event settingsObligationEvent, to settingsObligationState) error {
			if event == settingsEvtSaveSucceeded && to != settingsObligationClean {
				return errors.New("a successful write is the only discharge to clean")
			}
			if event == settingsEvtSaveFailed && to != settingsObligationRetry {
				return errors.New("a failed write must stay owed")
			}
			if to == settingsObligationClean && event != settingsEvtSaveSucceeded &&
				event != settingsEvtConfirmUnchanged {
				return errors.New("only a successful write (or a clean no-op confirm) reaches clean")
			}
			return nil
		},
	})
}

// themePreviewState models the settings dialog's live theme preview across
// open/preview/Esc/confirm (settingsThemeOriginal /
// settingsThemeDirty / applyTheme). The theme IDs and the settingsSession
// generation fencing stale previews are extended state.
type themePreviewState uint8

const (
	// themeAtOriginal: the live theme equals the theme active when the
	// dialog opened.
	themeAtOriginal themePreviewState = iota
	// themeDiverged: a preview (or an in-dialog selection) moved the live
	// theme away from the open-time original.
	themeDiverged
	// themeKept: the dialog confirmed with the divergence kept — the live
	// theme survives; the obligation is the section machine's business.
	themeKept
)

func (s themePreviewState) String() string {
	switch s {
	case themeDiverged:
		return "diverged"
	case themeKept:
		return "kept"
	default:
		return "at-original"
	}
}

// themePreviewEvent is the theme machine's alphabet.
type themePreviewEvent uint8

const (
	// themeEvtOpen: the dialog opened — the open-time original is recorded.
	themeEvtOpen themePreviewEvent = iota
	// themeEvtPreview: a live preview applied (generation-checked).
	themeEvtPreview
	// themeEvtEscRestore: Esc — revert to the open-time original, persist
	// nothing.
	themeEvtEscRestore
	// themeEvtConfirmKeep: confirm — keep the live theme.
	themeEvtConfirmKeep
)

func (e themePreviewEvent) String() string {
	switch e {
	case themeEvtOpen:
		return "open"
	case themeEvtPreview:
		return "preview"
	case themeEvtEscRestore:
		return "esc-restore"
	default:
		return "confirm-keep"
	}
}

// themePreviewMachine is the live-theme table: Esc always restores the
// open-time original, confirm never does (the user's divergence is kept and
// becomes a save obligation), and a later dialog re-records the original
// fresh.
var themePreviewMachine = fsm.MustCompile(fsm.Machine[themePreviewState, themePreviewEvent]{
	Name:    "settings-theme-preview",
	Initial: themeAtOriginal,
	Steps: []fsm.Step[themePreviewState, themePreviewEvent]{
		{From: themeAtOriginal, Event: themeEvtPreview, To: themeDiverged},
		{From: themeDiverged, Event: themeEvtPreview, To: themeDiverged},
		{From: themeDiverged, Event: themeEvtEscRestore, To: themeAtOriginal},
		{From: themeDiverged, Event: themeEvtConfirmKeep, To: themeKept},
		{From: themeKept, Event: themeEvtOpen, To: themeAtOriginal},
		{From: themeAtOriginal, Event: themeEvtOpen, To: themeAtOriginal},
	},
})

// themePreviewCheck formally verifies the theme flow: Esc always lands on
// the open-time original (a cancel never leaks a preview), confirm-keep
// never lands on the original (the kept divergence becomes the section
// machine's obligation), and the kept state is only exited by a fresh
// dialog open re-recording the original.
func themePreviewCheck() error {
	return themePreviewMachine.Check(fsm.Checker[themePreviewState, themePreviewEvent]{
		Alphabet: []themePreviewEvent{
			themeEvtOpen, themeEvtPreview, themeEvtEscRestore, themeEvtConfirmKeep,
		},
		AllDeclaredReachable: true,
		Edge: func(_ themePreviewState, event themePreviewEvent, to themePreviewState) error {
			if event == themeEvtEscRestore && to != themeAtOriginal {
				return errors.New("an Esc restore must return the open-time original theme")
			}
			if event == themeEvtConfirmKeep && to == themeAtOriginal {
				return errors.New("confirm must keep the live theme, not restore the original")
			}
			return nil
		},
	})
}
