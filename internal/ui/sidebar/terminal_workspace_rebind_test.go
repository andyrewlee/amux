package sidebar

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestRebindWorkspaceIDMigratesTerminalState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}
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
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected workspace IDs to differ: old=%q new=%q", oldWS.ID(), newWS.ID())
	}

	m := NewTerminalModel()
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	tab := &TerminalTab{
		ID:   TerminalTabID("term-tab-1"),
		Name: "Terminal 1",
		State: &TerminalState{
			Running: false,
		},
	}
	m.tabs.ByWorkspace[oldID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0
	m.markPendingCreation(oldID)

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for non-running terminal")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	gotTabs := m.tabs.ByWorkspace[newID]
	if len(gotTabs) != 1 || gotTabs[0] != tab {
		t.Fatalf("expected migrated terminal tab under new workspace key, got %d", len(gotTabs))
	}
	if got := m.tabs.ActiveByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.tabs.ActiveByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
	if _, ok := m.pendingCreation[newID]; !ok {
		t.Fatalf("expected pending creation flag to migrate to %q", newID)
	}
	if _, ok := m.pendingCreation[oldID]; ok {
		t.Fatalf("expected pending creation flag to be removed from %q", oldID)
	}
}

// TestRebindWorkspaceIDOrphanedFlushTick mirrors the center-pane regression:
// a flush tick scheduled before a workspace rebind carries the old ID, and the
// exact-key lookup in handlePTYFlush would miss — latching FlushScheduled
// forever with no recovery path. Rebind must unlatch and re-arm under the new
// ID, and the orphaned tick must still resolve via the ID-only fallback.
func TestRebindWorkspaceIDOrphanedFlushTick(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &TerminalTab{
		ID:   TerminalTabID("term-tab-1"),
		Name: "Terminal 1",
		State: &TerminalState{
			Running: false,
		},
	}
	tab.State.PendingOutput = []byte("buffered output")
	tab.State.FlushScheduled = true
	tab.State.FlushPendingSince = time.Now()
	tab.State.LastOutputAt = time.Now()
	m.tabs.ByWorkspace[oldID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd == nil {
		t.Fatal("expected rebind to re-arm a flush tick for migrated pending output")
	}
	if !tab.State.FlushScheduled {
		t.Fatal("expected FlushScheduled re-latched for the re-armed new-ID tick")
	}

	if flushCmd := m.handlePTYFlush(messages.SidebarPTYFlush{WorkspaceID: oldID, TabID: string(tab.ID)}); flushCmd == nil {
		t.Fatal("orphaned SidebarPTYFlush was dropped — stale workspace ID did not fall back to the tab")
	}
}

// TestRebindWorkspaceIDOrphanedStoppedMsg: a SidebarPTYStopped stamped with
// the old workspace ID must still find the migrated tab and mark it detached.
func TestRebindWorkspaceIDOrphanedStoppedMsg(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &TerminalTab{
		ID:   TerminalTabID("term-tab-1"),
		Name: "Terminal 1",
		State: &TerminalState{
			Running: true,
		},
	}
	m.tabs.ByWorkspace[oldID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	m.RebindWorkspaceID(oldWS, newWS)

	m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: oldID, TabID: string(tab.ID)})
	if !tab.State.Detached {
		t.Fatal("orphaned SidebarPTYStopped was dropped — tab not marked detached")
	}
}

// rebindWorkspacePair returns two workspaces whose computed IDs differ (the
// rel-vs-abs repo/root path split that triggers rebinding in production).
func rebindWorkspacePair(t *testing.T) (*data.Workspace, *data.Workspace) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}
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
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected workspace IDs to differ: old=%q new=%q", oldWS.ID(), newWS.ID())
	}
	return oldWS, newWS
}

// TestHandleTerminalCreatedStaleWorkspaceID covers the create-result half of
// the stale-workspace-ID bug: a SidebarTerminalCreated stamped with the
// pre-rebind ID must file under the live key and clear the migrated
// pendingCreation flag — otherwise the flag wedges forever and the created
// tab runs invisibly under a dead bucket.
func TestHandleTerminalCreatedStaleWorkspaceID(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = newWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	m.markPendingCreation(newID) // the flag the rebind migrated

	m.handleTerminalCreated(SidebarTerminalCreated{
		WorkspaceID: oldID,
		TabID:       TerminalTabID("term-tab-1"),
	})

	if _, ok := m.pendingCreation[newID]; ok {
		t.Fatal("pendingCreation still set under current workspace — auto-create would wedge")
	}
	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatal("dead workspace bucket resurrected")
	}
	tabs := m.tabs.ByWorkspace[newID]
	if len(tabs) != 1 || tabs[0].ID != TerminalTabID("term-tab-1") {
		t.Fatalf("tab not filed under current workspace %s: %+v", newID, m.tabs.ByWorkspace)
	}
}

// TestHandleTerminalCreatedDroppedWorkspace covers post-delete arrival: a
// create result whose workspace is gone must be dropped (PTY closed), not
// filed under an unrelated workspace or a resurrected bucket.
func TestHandleTerminalCreatedDroppedWorkspace(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = newWS // a different workspace is current; nothing pending

	m.handleTerminalCreated(SidebarTerminalCreated{
		WorkspaceID: string(oldWS.ID()),
		TabID:       TerminalTabID("term-tab-1"),
	})

	if len(m.tabs.ByWorkspace) != 0 {
		t.Fatalf("orphaned create result filed tabs: %+v", m.tabs.ByWorkspace)
	}
}

// TestHandleCreateFailedStaleWorkspaceID: a create failure stamped with the
// pre-rebind ID must still clear the pending flag under the live key, or
// auto-create wedges forever.
func TestHandleCreateFailedStaleWorkspaceID(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = newWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	m.markPendingCreation(newID)

	m.handleCreateFailed(SidebarTerminalCreateFailed{WorkspaceID: oldID, Err: errors.New("boom")})

	if _, ok := m.pendingCreation[newID]; ok {
		t.Fatal("pendingCreation still set under current workspace after stale failure")
	}
}

// TestRebindWorkspaceIDOrphanedOutputMsg mirrors the center-pane regression:
// a SidebarPTYOutput read before a workspace rebind but delivered after it
// carries the old ID — an exact-key lookup misses the migrated bucket and
// silently discards the payload, a hole in the terminal byte stream. The
// ID-only fallback must route the bytes to the migrated tab.
func TestRebindWorkspaceIDOrphanedOutputMsg(t *testing.T) {
	oldWS, newWS := rebindWorkspacePair(t)

	m := NewTerminalModel()
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &TerminalTab{
		ID:   TerminalTabID("term-tab-1"),
		Name: "Terminal 1",
		State: &TerminalState{
			Running: true,
		},
	}
	m.tabs.ByWorkspace[oldID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	m.RebindWorkspaceID(oldWS, newWS)

	_ = m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: oldID, TabID: string(tab.ID), Data: []byte("orphaned-bytes")})
	if got := string(tab.State.PendingOutput); !strings.Contains(got, "orphaned-bytes") {
		t.Fatalf("orphaned SidebarPTYOutput was dropped — stale workspace ID did not fall back to the tab (PendingOutput=%q)", got)
	}
}
