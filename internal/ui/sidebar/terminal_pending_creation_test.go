package sidebar

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestPendingCreationExpiresAfterTimeout(t *testing.T) {
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{})
	wsID := m.workspaceID()

	// Fresh mark gates.
	m.markPendingCreation(wsID)
	if !m.pendingCreationActive(wsID) {
		t.Fatal("fresh pendingCreation mark not reported active")
	}
	if cmd := m.EnsureTerminalTab(); cmd != nil {
		t.Fatal("fresh pendingCreation should still gate EnsureTerminalTab")
	}

	// Backdate past the timeout — the mark must expire in place.
	m.pendingCreation[wsID] = time.Now().Add(-pendingCreationTimeout - time.Second)
	if m.pendingCreationActive(wsID) {
		t.Fatal("stale pendingCreation mark still reported active")
	}
	if _, ok := m.pendingCreation[wsID]; ok {
		t.Fatal("expired mark left in the map — would re-wedge")
	}
}

// TestPendingCreationRebindPreservesStartTime: a rebind migrates the mark's
// timestamp, so an old mark can't ride a rebind to a fresh lease.
func TestPendingCreationRebindPreservesStartTime(t *testing.T) {
	wd := t.TempDir()
	absRepo := filepath.Join(wd, "repo")
	absRoot := filepath.Join(wd, "repo", "ws")
	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}
	oldWS := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	newWS := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)

	m := NewTerminalModel()
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	m.pendingCreation[oldID] = time.Now().Add(-pendingCreationTimeout - time.Second)

	m.RebindWorkspaceID(oldWS, newWS)

	// The stale mark must not migrate — it expires at the check rather than
	// wedging the new ID.
	if _, ok := m.pendingCreation[newID]; ok {
		t.Fatal("stale mark migrated under the new ID — it should expire instead")
	}
	if _, ok := m.pendingCreation[oldID]; ok {
		t.Fatal("stale mark left under the old ID")
	}
}
