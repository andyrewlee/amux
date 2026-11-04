package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// persistSlotState models one workspace's tab-persistence slot through the
// debounce loop in app_persistence.go. The dirty map, persistToken
// generations, and the capture sequence (a.lifecycle.persistSeq) are
// extended state; this table is the verified shape around them. The
// load-bearing properties are the two the code comments defend by hand: a
// failed save must fall back to dirty (never silently clean, never stuck in
// a failed state), and a slot only returns to clean through an actual save
// result — a superseded debounce tick never cleans anything by itself.
type persistSlotState uint8

const (
	// persistClean: no unsaved tab state.
	persistClean persistSlotState = iota
	// persistDirty: marked dirty; a debounce tick is armed.
	persistDirty
	// persistSaving: snapshots captured and the save cmd is in flight (the
	// dirty markers for the captured set were cleared on the Update loop).
	persistSaving
)

func (s persistSlotState) String() string {
	switch s {
	case persistDirty:
		return "dirty"
	case persistSaving:
		return "saving"
	default:
		return "clean"
	}
}

// persistSlotEvent is the persist machine's alphabet.
type persistSlotEvent uint8

const (
	// persistEvtMutate: tab state changed — mark dirty, (re-)arm the
	// debounce. Also the mutation-in-flight retention path: a mutate while
	// saving keeps the slot owed.
	persistEvtMutate persistSlotEvent = iota
	// persistEvtDebounceFired: the debounce tick arrived with the current
	// token — capture snapshots and start the save.
	persistEvtDebounceFired
	// persistEvtDebounceSuperseded: a stale tick (a newer request bumped
	// the token) — dropped; the newer request's debounce owns the timer.
	persistEvtDebounceSuperseded
	// persistEvtSaveSucceeded: the save wrote and nothing re-dirtied the
	// slot meanwhile.
	persistEvtSaveSucceeded
	// persistEvtSaveSucceededRedirty: the save wrote, but mutations arrived
	// mid-flight and re-marked the slot — their debounce is already armed.
	persistEvtSaveSucceededRedirty
	// persistEvtSaveFailed: the save (or a panicking save batch) failed —
	// handlePersistSaveFailed re-marks dirty on the Update loop so the next
	// debounce retries.
	persistEvtSaveFailed
	// persistEvtShutdownFlush: persistAllWorkspacesNow saved everything
	// synchronously and cleared the dirty set.
	persistEvtShutdownFlush
)

func (e persistSlotEvent) String() string {
	switch e {
	case persistEvtMutate:
		return "mutate"
	case persistEvtDebounceFired:
		return "debounce-fired"
	case persistEvtDebounceSuperseded:
		return "debounce-superseded"
	case persistEvtSaveSucceeded:
		return "save-succeeded"
	case persistEvtSaveSucceededRedirty:
		return "save-succeeded-redirty"
	case persistEvtSaveFailed:
		return "save-failed"
	default:
		return "shutdown-flush"
	}
}

// persistSlotMachine is the per-workspace persist-debounce table.
var persistSlotMachine = fsm.MustCompile(fsm.Machine[persistSlotState, persistSlotEvent]{
	Name:    "tab-persist-debounce",
	Initial: persistClean,
	Steps: []fsm.Step[persistSlotState, persistSlotEvent]{
		{From: persistClean, Event: persistEvtMutate, To: persistDirty},
		{From: persistDirty, Event: persistEvtMutate, To: persistDirty},
		{From: persistDirty, Event: persistEvtDebounceFired, To: persistSaving},
		{From: persistDirty, Event: persistEvtDebounceSuperseded, To: persistDirty},
		{From: persistSaving, Event: persistEvtMutate, To: persistSaving},
		{From: persistSaving, Event: persistEvtSaveSucceeded, To: persistClean},
		{From: persistSaving, Event: persistEvtSaveSucceededRedirty, To: persistDirty},
		{From: persistSaving, Event: persistEvtSaveFailed, To: persistDirty},
		{From: persistDirty, Event: persistEvtShutdownFlush, To: persistClean},
		{From: persistClean, Event: persistEvtShutdownFlush, To: persistClean},
	},
})

// persistSlotCheck formally verifies the debounce loop: a failed save
// always returns to dirty (the retry obligation is never dropped and never
// wedges), the only edges into clean are a successful save or the shutdown
// flush (a superseded tick or a mid-flight mutation can never clean a
// slot), and superseded ticks change nothing.
func persistSlotCheck() error {
	return persistSlotMachine.Check(fsm.Checker[persistSlotState, persistSlotEvent]{
		Alphabet: []persistSlotEvent{
			persistEvtMutate, persistEvtDebounceFired, persistEvtDebounceSuperseded,
			persistEvtSaveSucceeded, persistEvtSaveSucceededRedirty, persistEvtSaveFailed,
			persistEvtShutdownFlush,
		},
		AllDeclaredReachable: true,
		Edge: func(_ persistSlotState, event persistSlotEvent, to persistSlotState) error {
			switch event {
			case persistEvtSaveFailed:
				if to != persistDirty {
					return errors.New("a failed save must return to dirty for retry")
				}
			case persistEvtDebounceSuperseded:
				if to != persistDirty {
					return errors.New("a superseded debounce tick must leave the slot exactly as it was")
				}
			case persistEvtSaveSucceeded, persistEvtShutdownFlush:
				if to != persistClean && to != persistDirty {
					return errors.New("a completed save lands on clean or back on dirty (mid-flight mutations)")
				}
			default:
				if to == persistClean {
					return errors.New("only a save result or the shutdown flush may clean a slot")
				}
			}
			return nil
		},
	})
}
