package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// The reported-status badge behavior: entering blocked latches the badge and
// rings the bell unconditionally; viewing the workspace acks it; a fresh
// blocked publish re-raises; a working publish clears it without ack.

func TestReportedBlockedBellsAndBadges(t *testing.T) {
	m := New()
	m.SetNotifyOnDone(false) // the blocked bell is not preference-gated

	// Seed a working publish so the blocked edge is a real transition.
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking}); cmd != nil {
		t.Fatal("working publish must not bell")
	}
	if m.blockedBadgeVisible("ws-1") {
		t.Fatal("no blocked badge before a report")
	}

	cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked})
	if cmd == nil {
		t.Fatal("entering reported blocked must ring the bell")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("reported blocked must latch the badge")
	}

	// Re-publishing blocked is a self-loop: no second bell, badge persists.
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd != nil {
		t.Fatal("re-observation of blocked must not re-bell")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("badge must survive re-observation")
	}
}

func TestReportedBlockedAckAndRearm(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked})

	m.ackDone("ws-1")
	if m.blockedBadgeVisible("ws-1") {
		t.Fatal("ack must suppress the blocked badge")
	}

	// A fresh blocked publish after the ack re-raises and re-rings.
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking}); cmd != nil {
		t.Fatal("working publish must not bell")
	}
	if m.blockedBadgeVisible("ws-1") {
		t.Fatal("working publish must clear the blocked latch")
	}
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd == nil {
		t.Fatal("re-blocked after re-arm must ring again")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("re-blocked must re-raise the badge")
	}
}

func TestReportedBlockedAckedReblockWithoutWorking(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked})
	m.ackDone("ws-1")
	// The agent answers its own question by blocking on the NEXT thing
	// without an observable working window between: the new blocked edge
	// still re-raises.
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateIdle}); cmd != nil {
		t.Fatal("idle publish must not bell")
	}
	if m.blockedBadgeVisible("ws-1") {
		t.Fatal("idle publish (program moved on) must clear the badge")
	}
}

func TestReportedErrorSharesTheBlockedLatch(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateError}); cmd == nil {
		t.Fatal("reported error must ring")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("reported error must latch the badge")
	}
}

// TestReportedBlockedAckedReobservationParks pins the re-observation rule:
// after the user acks a blocked workspace, the scan re-reads the SAME
// stored report every tick — a re-observation, not an edge — and it must
// neither re-raise the badge nor re-ring. Only a genuinely fresh blocked
// edge (a different live state in between) interrupts again.
func TestReportedBlockedAckedReobservationParks(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked})
	m.ackDone("ws-1")
	if m.blockedBadgeVisible("ws-1") {
		t.Fatal("precondition: ack must suppress the badge")
	}
	// Every scan tick re-publishes the identical map.
	for i := 0; i < 5; i++ {
		if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd != nil {
			t.Fatal("re-observation of the same blocked report must not re-bell")
		}
		if m.blockedBadgeVisible("ws-1") {
			t.Fatal("re-observation must not re-raise the acked badge")
		}
	}
	// A real edge (the program went working in between, then blocked again)
	// still re-raises and re-rings.
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd == nil {
		t.Fatal("a fresh blocked edge after a working window must ring again")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("a fresh blocked edge must re-raise the badge")
	}
}

// TestReportedBlockedSurvivesHandoffIdlePublish pins the latch's survival
// across the empty re-publish an ownership handoff emits (tmux ownership
// change or server blip republishes nil, stepping every workspace to
// Idle): the badge must not vanish for the handoff window, and the new
// owner's first scan must not re-bell the still-blocked workspace.
func TestReportedBlockedSurvivesHandoffIdlePublish(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked})
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("precondition: blocked badge latched")
	}
	// The handoff window: nothing known, everything steps to Idle.
	if cmd := m.SetAgentStates(nil); cmd != nil {
		t.Fatal("an empty re-publish must not bell")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("the blocked badge must survive the handoff's empty publish")
	}
	// The new owner's first scan re-publishes blocked from an Idle prev.
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd != nil {
		t.Fatal("re-publishing the still-blocked workspace after handoff must not re-bell")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("the badge must still render after the new owner's first scan")
	}
}

// TestFreshBlockedLatchesOverBell pins the precedence: when the program
// itself reports blocked while the generic attention (BEL) latch is set,
// the blocked badge renders — the attention badge must not mask the
// program's own needs-user signal.
func TestFreshBlockedLatchesOverBell(t *testing.T) {
	m := New()
	m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateWorking})
	m.MarkAttention("ws-1")
	if !m.attentionFlagged("ws-1") || m.blockedBadgeVisible("ws-1") {
		t.Fatal("precondition: attention latch set, no blocked badge")
	}
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-1": data.StateBlocked}); cmd == nil {
		t.Fatal("a fresh blocked publish must ring")
	}
	if !m.blockedBadgeVisible("ws-1") {
		t.Fatal("blocked must latch over the bell badge")
	}
}

// TestAttentionMachineFormallyChecked already covers the table; AllDeclared
// Reachable means the blocked states keep participating: dropping every
// blocked edge would strand attBlocked/attBlockedAcked unreachable and fail
// the check, so this stays a one-line regression pin.
func TestAttentionMachineChecksBlockedEdges(t *testing.T) {
	if err := attentionMachineCheck(); err != nil {
		t.Fatalf("attention machine check failed: %v", err)
	}
}
