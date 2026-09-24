package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// assertBell fails unless cmd produces exactly the terminal-bell RawMsg.
func assertBell(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a bell command, got nil")
	}
	msg := cmd()
	raw, ok := msg.(tea.RawMsg)
	if !ok {
		t.Fatalf("expected tea.RawMsg, got %T", msg)
	}
	if raw.Msg != bellSequence {
		t.Fatalf("expected bell sequence %q, got %q", bellSequence, raw.Msg)
	}
}

// assertNoBell fails if cmd is non-nil (any command at all counts as a bell,
// since SetAgentStates only ever emits the bell command).
func assertNoBell(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		t.Fatalf("expected no bell command, got %T producing %#v", cmd, cmd())
	}
}

const notifyWS = "ws-1"

func TestNotifyOnDoneBellOnEnabledEdge(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	// Working frame primes the previous state; no edge yet.
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking}))

	// Working→Done edge: bell fires exactly once.
	assertBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
}

func TestNotifyOnDoneNoBellOnSteadyDone(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))

	// Done→Done (no fresh edge): must not re-bell, even across several frames.
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
}

func TestNotifyOnDoneNoBellWhenDisabled(t *testing.T) {
	m := New()
	// notifyOnDone defaults off; do not enable it.

	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking}))
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
}

func TestNotifyOnDoneNoBellOnIdleToDone(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	// First observation is already Done (agent never seen Working): not a
	// finish the user watched, so no bell.
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
}

func TestNotifyOnDoneReBellsOnFreshWorkCycle(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))

	// Ack the done indicator (user viewed the row).
	m.ackDone(notifyWS)
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))

	// A fresh work cycle: Working clears the ack, and the next Working→Done edge
	// bells again.
	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone}))
}

func TestNotifyOnDoneSingleBellForSimultaneousEdges(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]data.AgentState{
		"ws-a": data.StateWorking,
		"ws-b": data.StateWorking,
	}))

	// Both finish in the same frame: one bell, not two.
	assertBell(t, m.SetAgentStates(map[string]data.AgentState{
		"ws-a": data.StateDone,
		"ws-b": data.StateDone,
	}))
}

func TestDoneLatchSurvivesDecay(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone})
	if !m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should be visible on the Working→Done edge")
	}

	// Past DoneWindow, ClassifyState reports Idle — the badge must persist
	// on the latch, not decay with the classified state.
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateIdle})
	if !m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should survive Done→Idle decay (latched)")
	}
	// And when the workspace drops out of the state map entirely.
	m.SetAgentStates(map[string]data.AgentState{})
	if !m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should persist while the workspace is absent from the map")
	}

	// Ack (row activation) clears it.
	m.ackDone(notifyWS)
	if m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should clear on ack")
	}
}

func TestDoneLatchClearsOnReWorking(t *testing.T) {
	m := New()

	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone})
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateIdle})
	if !m.doneBadgeVisible(notifyWS) {
		t.Fatal("precondition: latched")
	}

	// A new work cycle clears the latch so the next Done edge re-badges.
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateWorking})
	if m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should clear on re-Working")
	}
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone})
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateIdle})
	if !m.doneBadgeVisible(notifyWS) {
		t.Fatal("done badge should re-latch on a fresh Working→Done edge")
	}
}

func TestDoneLatchNoLatchOnIdleToDone(t *testing.T) {
	m := New()

	// An Idle→Done observation (never seen Working) is not a finish the user
	// watched — same reason the bell doesn't fire, no latch is set.
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateDone})
	m.SetAgentStates(map[string]data.AgentState{notifyWS: data.StateIdle})
	if m.doneBadgeVisible(notifyWS) {
		t.Fatal("Idle→Done should not latch the badge")
	}
}
