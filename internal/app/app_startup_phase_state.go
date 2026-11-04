package app

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// startupPhase models the app's startup gate as three independent results
// that must all land before the app is fully running: the first
// WindowSizeMsg (App.ready), the tmux availability check
// (tmuxCheckDone/tmuxAvailable), and the initial projects load
// (projectsLoaded). The three-bit state space is exactly the machine —
// each bit's event is legal whenever the bit is unset, so the tmux check
// and the projects load may arrive in either order, and whichever of the
// three results arrives last reconciles the app into running (the comment
// at handleTmuxAvailableResult: "whichever result arrives second performs
// reconciliation immediately"). Timestamps, tokens, and the
// projectsLoadToken stale-drop are extended state at the call sites.
type startupPhase uint8

const (
	// startupInit: nothing has arrived yet.
	startupInit startupPhase = iota
	// startupSized: only the WindowSizeMsg has arrived.
	startupSized
	// startupTmuxChecked: only the tmux check has arrived (before the size).
	startupTmuxChecked
	// startupProjectsLoaded: only the projects load has arrived.
	startupProjectsLoaded
	// startupAwaitingProjects: size + tmux known; waiting on the load.
	startupAwaitingProjects
	// startupAwaitingTmux: size + projects known; waiting on the check.
	startupAwaitingTmux
	// startupAwaitingSize: both async results known; waiting on the size.
	startupAwaitingSize
	// startupRunning: all three known — input, scans, and GC are ungated.
	startupRunning
	// startupQuitting: the quit dialog confirmed (dialogResultQuit).
	startupQuitting
	// startupDone: shutdown complete (terminal).
	startupDone
)

func (p startupPhase) String() string {
	switch p {
	case startupSized:
		return "sized"
	case startupTmuxChecked:
		return "tmux-checked"
	case startupProjectsLoaded:
		return "projects-loaded"
	case startupAwaitingProjects:
		return "awaiting-projects"
	case startupAwaitingTmux:
		return "awaiting-tmux"
	case startupAwaitingSize:
		return "awaiting-size"
	case startupRunning:
		return "running"
	case startupQuitting:
		return "quitting"
	case startupDone:
		return "done"
	default:
		return "init"
	}
}

// startupEvent is the startup machine's alphabet.
type startupEvent uint8

const (
	// startupEvtSize: the first WindowSizeMsg (handleWindowSize sets ready).
	startupEvtSize startupEvent = iota
	// startupEvtTmuxChecked: tmuxAvailableResult applied (settle counters
	// reset alongside — see the activity-settle machine below).
	startupEvtTmuxChecked
	// startupEvtProjectsLoaded: ProjectsLoaded applied (handleProjectsLoaded
	// sets projectsLoaded after the stale-token drop).
	startupEvtProjectsLoaded
	// startupEvtQuit: the quit confirmation set quitting.
	startupEvtQuit
	// startupEvtShutdownDone: Shutdown ran (shutdownOnce).
	startupEvtShutdownDone
)

func (e startupEvent) String() string {
	switch e {
	case startupEvtSize:
		return "size"
	case startupEvtTmuxChecked:
		return "tmux-checked"
	case startupEvtProjectsLoaded:
		return "projects-loaded"
	case startupEvtQuit:
		return "quit"
	default:
		return "shutdown-done"
	}
}

// startupMachine is the startup sequencing gate. The three result events
// are legal from every state that still lacks their bit, and running is
// entered exactly when the last one lands — from any of the three
// awaiting states. Quit runs from the running state (the quit dialog is
// reached through live input); done is terminal.
var startupMachine = fsm.MustCompile(fsm.Machine[startupPhase, startupEvent]{
	Name:      "app-startup",
	Initial:   startupInit,
	Terminals: []startupPhase{startupDone},
	Steps: []fsm.Step[startupPhase, startupEvent]{
		{From: startupInit, Event: startupEvtSize, To: startupSized},
		{From: startupInit, Event: startupEvtTmuxChecked, To: startupTmuxChecked},
		{From: startupInit, Event: startupEvtProjectsLoaded, To: startupProjectsLoaded},
		{From: startupSized, Event: startupEvtTmuxChecked, To: startupAwaitingProjects},
		{From: startupSized, Event: startupEvtProjectsLoaded, To: startupAwaitingTmux},
		{From: startupTmuxChecked, Event: startupEvtSize, To: startupAwaitingProjects},
		{From: startupTmuxChecked, Event: startupEvtProjectsLoaded, To: startupAwaitingSize},
		{From: startupProjectsLoaded, Event: startupEvtSize, To: startupAwaitingTmux},
		{From: startupProjectsLoaded, Event: startupEvtTmuxChecked, To: startupAwaitingSize},
		{From: startupAwaitingProjects, Event: startupEvtProjectsLoaded, To: startupRunning},
		{From: startupAwaitingTmux, Event: startupEvtTmuxChecked, To: startupRunning},
		{From: startupAwaitingSize, Event: startupEvtSize, To: startupRunning},
		{From: startupRunning, Event: startupEvtQuit, To: startupQuitting},
		{From: startupQuitting, Event: startupEvtShutdownDone, To: startupDone},
	},
})

// startupCheck formally verifies the startup gate: running is reachable
// through all three arrival orders (each awaiting state has its own
// completing edge — whichever result arrives second reconciles), no event
// ever leaves running except quit (the gate is one-way: a later reload or
// recheck does not un-run the app), and done is a true terminal.
func startupCheck() error {
	return startupMachine.Check(fsm.Checker[startupPhase, startupEvent]{
		Alphabet: []startupEvent{
			startupEvtSize, startupEvtTmuxChecked, startupEvtProjectsLoaded,
			startupEvtQuit, startupEvtShutdownDone,
		},
		AllDeclaredReachable: true,
		Edge: func(from startupPhase, event startupEvent, to startupPhase) error {
			if from == startupRunning && event != startupEvtQuit {
				return fmt.Errorf("no path back from running (illegal %s)", event)
			}
			if to == startupRunning && from != startupAwaitingProjects && from != startupAwaitingTmux && from != startupAwaitingSize {
				return fmt.Errorf("running is entered only when the LAST awaited result lands (illegal %s --%s--> running)", from, event)
			}
			return nil
		},
	})
}

// activitySettleState models the post-startup scan-settling gate
// (tmuxActivity.settled / settledScans): activity is not trusted until
// tmuxActivitySettleScans consecutive scans have been applied, and the
// counter restarts on a tmux recheck or an owner transition so a transient
// empty set is never published as authoritative. The counter itself is
// extended state.
type activitySettleState uint8

const (
	// activitySettling: scans accumulate toward the threshold.
	activitySettling activitySettleState = iota
	// activitySettled: the threshold was reached; activity is trusted.
	activitySettled
)

func (s activitySettleState) String() string {
	if s == activitySettled {
		return "settled"
	}
	return "settling"
}

// activitySettleEvent is the settle machine's alphabet.
type activitySettleEvent uint8

const (
	// settleEvtScanApplied: an applied scan (threshold not yet reached).
	settleEvtScanApplied activitySettleEvent = iota
	// settleEvtThresholdReached: the applied scan crossed
	// tmuxActivitySettleScans.
	settleEvtThresholdReached
	// settleEvtTmuxRecheck: handleTmuxAvailableResult reset the counters.
	settleEvtTmuxRecheck
	// settleEvtOwnerTransition: a follower→owner (or epoch) transition
	// reset the counters in updateTmuxActivityOwnershipState.
	settleEvtOwnerTransition
)

func (e activitySettleEvent) String() string {
	switch e {
	case settleEvtScanApplied:
		return "scan-applied"
	case settleEvtThresholdReached:
		return "threshold-reached"
	case settleEvtTmuxRecheck:
		return "tmux-recheck"
	default:
		return "owner-transition"
	}
}

// activitySettleMachine is the settle gate: only applied scans count, the
// threshold crossing is one-way until a reset event, and both resets
// (recheck and owner transition) always restart the accumulation.
var activitySettleMachine = fsm.MustCompile(fsm.Machine[activitySettleState, activitySettleEvent]{
	Name:    "activity-settle",
	Initial: activitySettling,
	Steps: []fsm.Step[activitySettleState, activitySettleEvent]{
		{From: activitySettling, Event: settleEvtScanApplied, To: activitySettling},
		{From: activitySettling, Event: settleEvtThresholdReached, To: activitySettled},
		{From: activitySettling, Event: settleEvtTmuxRecheck, To: activitySettling},
		{From: activitySettling, Event: settleEvtOwnerTransition, To: activitySettling},
		{From: activitySettled, Event: settleEvtScanApplied, To: activitySettled},
		{From: activitySettled, Event: settleEvtTmuxRecheck, To: activitySettling},
		{From: activitySettled, Event: settleEvtOwnerTransition, To: activitySettling},
	},
})

// activitySettleCheck formally verifies the settle gate: the threshold
// crossing only happens while settling, a settled gate is only ever
// un-settled by an explicit reset event (never silently by more scans),
// and both resets always restart the accumulation.
func activitySettleCheck() error {
	return activitySettleMachine.Check(fsm.Checker[activitySettleState, activitySettleEvent]{
		Alphabet: []activitySettleEvent{
			settleEvtScanApplied, settleEvtThresholdReached,
			settleEvtTmuxRecheck, settleEvtOwnerTransition,
		},
		AllDeclaredReachable: true,
		Edge: func(_ activitySettleState, event activitySettleEvent, to activitySettleState) error {
			if event == settleEvtThresholdReached && to != activitySettled {
				return errors.New("crossing the settle threshold must settle")
			}
			if (event == settleEvtTmuxRecheck || event == settleEvtOwnerTransition) && to != activitySettling {
				return errors.New("a settle reset must restart the accumulation")
			}
			return nil
		},
	})
}

// tmuxAvailableOrUnchecked is the single derived predicate behind the
// duplicated compound `!tmuxCheckDone || tmuxAvailable` guards (assistant
// auto-launch, prefix-palette entry visibility): an unfinished check counts
// as available — it settles during startup, long before a workspace can be
// created, and assuming otherwise would skip the launch. Once the check has
// run, the answer is exactly tmuxAvailable.
func (a *App) tmuxAvailableOrUnchecked() bool {
	return !a.tmuxCheckDone || a.tmuxAvailable
}
