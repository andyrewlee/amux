package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
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
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateWorking}))

	// Working→Done edge: bell fires exactly once.
	assertBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
}

func TestNotifyOnDoneNoBellOnSteadyDone(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))

	// Done→Done (no fresh edge): must not re-bell, even across several frames.
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
}

func TestNotifyOnDoneNoBellWhenDisabled(t *testing.T) {
	m := New()
	// notifyOnDone defaults off; do not enable it.

	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateWorking}))
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
}

func TestNotifyOnDoneNoBellOnIdleToDone(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	// First observation is already Done (agent never seen Working): not a
	// finish the user watched, so no bell.
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
}

func TestNotifyOnDoneReBellsOnFreshWorkCycle(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))

	// Ack the done indicator (user viewed the row).
	m.ackDone(notifyWS)
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))

	// A fresh work cycle: Working clears the ack, and the next Working→Done edge
	// bells again.
	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateWorking}))
	assertBell(t, m.SetAgentStates(map[string]activity.AgentState{notifyWS: activity.StateDone}))
}

func TestNotifyOnDoneSingleBellForSimultaneousEdges(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(true)

	assertNoBell(t, m.SetAgentStates(map[string]activity.AgentState{
		"ws-a": activity.StateWorking,
		"ws-b": activity.StateWorking,
	}))

	// Both finish in the same frame: one bell, not two.
	assertBell(t, m.SetAgentStates(map[string]activity.AgentState{
		"ws-a": activity.StateDone,
		"ws-b": activity.StateDone,
	}))
}
