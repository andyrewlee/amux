package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestSetAgentStatesDirtyGate pins the activity-sync render gate: a sync
// delivering an unchanged map must not bump contentVersion (every PTY-flush
// scan would otherwise force a content rebuild); a changed map must.
func TestSetAgentStatesDirtyGate(t *testing.T) {
	m := New()

	m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateWorking})
	after := m.contentVersion

	// Equal content in a fresh map — value equality, not pointer identity.
	m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateWorking})
	if m.contentVersion != after {
		t.Fatalf("unchanged states bumped contentVersion %d → %d", after, m.contentVersion)
	}

	m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateDone})
	if m.contentVersion != after+1 {
		t.Fatalf("changed states did not bump contentVersion exactly once: %d → %d", after, m.contentVersion)
	}

	// nil vs empty: both render identically — no bump.
	empty := m.contentVersion
	m.SetAgentStates(map[string]data.AgentState{})
	m.SetAgentStates(nil)
	m.SetAgentStates(map[string]data.AgentState{})
	if m.contentVersion != empty+1 {
		t.Fatalf("empty/nil transitions should bump once (non-empty→empty), got %d → %d", empty, m.contentVersion)
	}
}

// TestSetActiveWorkspacesDirtyGate covers the sibling sync setter fed by the
// same activity scan.
func TestSetActiveWorkspacesDirtyGate(t *testing.T) {
	m := New()

	m.SetActiveWorkspaces(map[string]bool{"ws-a": true})
	after := m.contentVersion

	m.SetActiveWorkspaces(map[string]bool{"ws-a": true})
	if m.contentVersion != after {
		t.Fatalf("unchanged active set bumped contentVersion %d → %d", after, m.contentVersion)
	}

	m.SetActiveWorkspaces(map[string]bool{"ws-a": true, "ws-b": true})
	if m.contentVersion != after+1 {
		t.Fatalf("changed active set did not bump contentVersion: %d → %d", after, m.contentVersion)
	}
}

// TestSetAgentStatesLatchRunsOnEqualMaps guards the boundary: the dirty gate
// must not skip the donePending/doneAcked bookkeeping on equal maps.
func TestSetAgentStatesLatchRunsOnEqualMaps(t *testing.T) {
	m := New()
	m.notifyOnDone = false

	m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateWorking})
	m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateDone})
	if !m.donePending["ws-a"] {
		t.Fatal("Working→Done edge did not latch donePending")
	}
	// Re-delivering the identical map must still run the latch loop without
	// dirtying — and must not re-fire the edge (prev is already Done).
	after := m.contentVersion
	if cmd := m.SetAgentStates(map[string]data.AgentState{"ws-a": data.StateDone}); cmd != nil {
		t.Fatal("equal Done map produced a bell cmd — steady-state re-edge")
	}
	if m.contentVersion != after {
		t.Fatal("equal Done map bumped contentVersion")
	}
	if !m.donePending["ws-a"] {
		t.Fatal("equal-map sync cleared donePending — latch logic must run unconditionally")
	}
}
