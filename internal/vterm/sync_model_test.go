package vterm

import (
	"fmt"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/fsm"
)

// syncState is the DEC 2026 synchronized-output window's control state.
type syncState int

const (
	syncOff syncState = iota
	syncOn
)

func (s syncState) String() string {
	if s == syncOn {
		return "sync-on"
	}
	return "sync-off"
}

// syncEvent is the sync window's alphabet.
type syncEvent int

const (
	evtSyncBegin syncEvent = iota
	evtSyncEnd
	evtSyncStallTimeout
)

func (e syncEvent) String() string {
	switch e {
	case evtSyncBegin:
		return "begin"
	case evtSyncEnd:
		return "end"
	default:
		return "stall-timeout"
	}
}

// syncMachine is the sync window protocol: begin opens the freeze; end
// closes it; a begin over an open window is a no-op (still one window); and
// the stall timeout force-closes a window whose end never arrived so a
// frozen frame can never persist forever.
var syncMachine = fsm.MustCompile(fsm.Machine[syncState, syncEvent]{
	Name:    "dec2026-sync-window",
	Initial: syncOff,
	Steps: []fsm.Step[syncState, syncEvent]{
		{From: syncOff, Event: evtSyncBegin, To: syncOn},
		{From: syncOn, Event: evtSyncBegin, To: syncOn},
		{From: syncOn, Event: evtSyncEnd, To: syncOff},
		{From: syncOn, Event: evtSyncStallTimeout, To: syncOff},
		{From: syncOff, Event: evtSyncEnd, To: syncOff},
		{From: syncOff, Event: evtSyncStallTimeout, To: syncOff},
	},
})

// TestSyncMachineFormallyChecked verifies the sync window table: sync-on is
// reachable, every close returns to sync-off, and no event other than begin
// can open a window.
func TestSyncMachineFormallyChecked(t *testing.T) {
	err := syncMachine.Check(fsm.Checker[syncState, syncEvent]{
		Alphabet:             []syncEvent{evtSyncBegin, evtSyncEnd, evtSyncStallTimeout},
		AllDeclaredReachable: true,
		Edge: func(from syncState, event syncEvent, to syncState) error {
			if to == syncOn && event != evtSyncBegin {
				return fmt.Errorf("only a begin may open a sync window (got %v)", event)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("sync window machine failed model checking: %v", err)
	}
}

// TestSyncRuntimeMatchesMachine differentially verifies the shipped
// setSynchronizedOutput/maybeReleaseStaleSync against the protocol machine:
// every event produces exactly the modeled transition, idempotent begins
// included.
func TestSyncRuntimeMatchesMachine(t *testing.T) {
	restore := syncNow
	defer func() { syncNow = restore }()

	clock := time.Now()
	syncNow = func() time.Time { return clock }

	runtimeState := func(v *VTerm) syncState {
		if v.SyncActive() {
			return syncOn
		}
		return syncOff
	}

	run := func(v *VTerm, event syncEvent) {
		switch event {
		case evtSyncBegin:
			v.setSynchronizedOutput(true)
		case evtSyncEnd:
			v.setSynchronizedOutput(false)
		case evtSyncStallTimeout:
			// Age the clock past the stall timeout, then run the same check
			// Write/RenderBuffers/Version perform.
			clock = clock.Add(2 * SyncStallTimeout)
			v.maybeReleaseStaleSync()
		}
	}

	for _, state := range []syncState{syncOff, syncOn} {
		for _, event := range syncMachine.Events() {
			modeled, ok := syncMachine.Next(state, event)
			if !ok {
				continue // total table; nothing is rejected
			}
			v := New(80, 24)
			if state == syncOn {
				v.setSynchronizedOutput(true)
			}
			run(v, event)
			if got := runtimeState(v); got != modeled {
				t.Errorf("runtime mismatch on %v --%v-->: got %v, model says %v", state, event, got, modeled)
			}
		}
	}
}
