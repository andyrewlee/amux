package center

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

func TestRebindWorkspaceIDMigratesTabState(t *testing.T) {
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

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	tab := &Tab{ID: TabID("tab-1"), Workspace: oldWS}
	m.tabs.ByWorkspace[oldID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for non-running tab")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	gotTabs := m.tabs.ByWorkspace[newID]
	if len(gotTabs) != 1 || gotTabs[0] != tab {
		t.Fatalf("expected migrated tab under new workspace key, got %d", len(gotTabs))
	}
	if gotTabs[0].Workspace != newWS {
		t.Fatal("expected migrated tab workspace pointer to be rebound")
	}
	if got := m.tabs.ActiveByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.tabs.ActiveByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
}

func TestRebindWorkspaceIDMigratesExplicitEmptyState(t *testing.T) {
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

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	m.tabs.ByWorkspace[oldID] = []*Tab{}
	m.tabs.ActiveByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for empty workspace state")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	if tabs, ok := m.tabs.ByWorkspace[newID]; !ok {
		t.Fatalf("expected migrated empty state under new workspace key %q", newID)
	} else if len(tabs) != 0 {
		t.Fatalf("expected migrated state to remain empty, got %d tabs", len(tabs))
	}
	if got := m.tabs.ActiveByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.tabs.ActiveByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
}

// TestRebindWorkspaceIDOrphanedFlushTick reproduces the freeze: a flush tick
// scheduled before a workspace rebind carries the old ID, and the exact-key
// lookup in updatePTYFlush would miss — leaving FlushScheduled latched forever
// and the pane frozen. The rebind must unlatch migrated tabs (re-arming a tick
// under the new ID when output is pending), and the orphaned tick itself must
// still resolve via the ID-only fallback.
func TestRebindWorkspaceIDOrphanedFlushTick(t *testing.T) {
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

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &Tab{ID: TabID("tab-1"), Workspace: oldWS}
	tab.PendingOutput = []byte("buffered output")
	tab.FlushScheduled = true
	tab.FlushPendingSince = time.Now()
	tab.LastOutputAt = time.Now()
	m.tabs.ByWorkspace[oldID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd == nil {
		t.Fatal("expected rebind to re-arm a flush tick for migrated pending output")
	}
	if !tab.FlushScheduled {
		t.Fatal("expected FlushScheduled re-latched for the re-armed new-ID tick")
	}

	// The in-flight tick stamped with the old ID must still reach the tab;
	// FlushGate defers (output is fresh) and returns a re-arm command.
	if flushCmd := m.updatePTYFlush(PTYFlush{WorkspaceID: oldID, TabID: tab.ID}); flushCmd == nil {
		t.Fatal("orphaned PTYFlush was dropped — stale workspace ID did not fall back to the tab")
	}
}

// TestRebindWorkspaceIDUnlatchesIdleTab covers the empty-buffer arm: a latched
// tab with nothing pending must simply come out of rebind unlatched so the
// next PTYOutput can schedule a flush.
func TestRebindWorkspaceIDUnlatchesIdleTab(t *testing.T) {
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

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &Tab{ID: TabID("tab-1"), Workspace: oldWS}
	tab.FlushScheduled = true
	tab.FlushPendingSince = time.Now()
	m.tabs.ByWorkspace[oldID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	if cmd := m.RebindWorkspaceID(oldWS, newWS); cmd != nil {
		t.Fatal("expected no re-arm for a migrated tab with empty PendingOutput")
	}
	if tab.FlushScheduled {
		t.Fatal("expected FlushScheduled cleared by rebind for an idle tab")
	}
	if !tab.FlushPendingSince.IsZero() {
		t.Fatal("expected FlushPendingSince cleared by rebind for an idle tab")
	}
}

// TestRebindWorkspaceIDOrphanedStoppedMsg covers the stopped/restart routing
// half of the fix: a PTYStopped stamped with the old workspace ID must still
// find the tab and mark it detached.
func TestRebindWorkspaceIDOrphanedStoppedMsg(t *testing.T) {
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

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &Tab{ID: TabID("tab-1"), Workspace: oldWS, Running: true}
	m.tabs.ByWorkspace[oldID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	m.RebindWorkspaceID(oldWS, newWS)

	// termAlive is false (no agent/terminal), so the stopped message must mark
	// the tab detached rather than silently dropping on the stale key.
	m.updatePTYStopped(PTYStopped{WorkspaceID: oldID, TabID: tab.ID})
	if !tab.Detached {
		t.Fatal("orphaned PTYStopped was dropped — tab not marked detached")
	}
}

// TestHandlePtyTabCreatedStaleWorkspaceRebind covers the center half of the
// stale-workspace-ID bug: a create result carrying the pre-rebind workspace
// object must file under the live key (canonical paths still match), not
// resurrect a dead bucket with an invisible running agent.
func TestHandlePtyTabCreatedStaleWorkspaceRebind(t *testing.T) {
	oldWS, newWS := centerRebindWorkspacePair(t)

	m := newTestModel()
	m.width = 20
	m.height = 9
	m.workspace = newWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())

	m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: oldWS,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-stale-ws"},
		TabID:     TabID("tab-stale-ws"),
	})

	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatal("tab filed under dead workspace key")
	}
	tabs := m.tabs.ByWorkspace[newID]
	if len(tabs) != 1 || tabs[0].ID != TabID("tab-stale-ws") {
		t.Fatalf("tab not filed under current workspace %s: %+v", newID, m.tabs.ByWorkspace)
	}
}

// TestHandlePtyTabCreatedDeletedWorkspaceDrops: a create result for a
// workspace that no longer exists must be dropped (agent closed), not filed
// under a resurrected bucket.
func TestHandlePtyTabCreatedDeletedWorkspaceDrops(t *testing.T) {
	oldWS, _ := centerRebindWorkspacePair(t)

	m := newTestModel()
	m.width = 20
	m.height = 9
	m.workspace = newTestWorkspace("other", "/other/root")
	// Simulate the delete landing between create dispatch and delivery.
	m.CleanupWorkspace(oldWS, nil)

	m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: oldWS,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-deleted-ws"},
		TabID:     TabID("tab-deleted-ws"),
	})

	if len(m.tabs.ByWorkspace) != 0 {
		t.Fatalf("orphaned create result filed tabs: %+v", m.tabs.ByWorkspace)
	}
}

// TestHandlePtyTabCreatedNonCurrentWorkspaceStillFiles: a create result for a
// workspace that exists but is no longer current (user switched between
// dispatch and delivery) keeps filing under the stamped key.
func TestHandlePtyTabCreatedNonCurrentWorkspaceStillFiles(t *testing.T) {
	oldWS, _ := centerRebindWorkspacePair(t)
	oldID := string(oldWS.ID())

	m := newTestModel()
	m.width = 20
	m.height = 9
	m.workspace = newTestWorkspace("other", "/other/root")

	m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: oldWS,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-switched-ws"},
		TabID:     TabID("tab-switched-ws"),
	})

	tabs := m.tabs.ByWorkspace[oldID]
	if len(tabs) != 1 || tabs[0].ID != TabID("tab-switched-ws") {
		t.Fatalf("tab not filed under stamped (non-current) workspace %s: %+v", oldID, m.tabs.ByWorkspace)
	}
}

// centerRebindWorkspacePair returns two workspaces whose computed IDs differ
// but whose canonical paths match — the rel-vs-abs split that triggers
// rebinding in production.
func centerRebindWorkspacePair(t *testing.T) (*data.Workspace, *data.Workspace) {
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

// TestRebindWorkspaceIDOrphanedOutputMsg reproduces the byte drop: a PTYOutput
// read before a workspace rebind but delivered after it carries the old ID —
// an exact-key lookup misses the migrated bucket and silently discards the
// payload, a hole in the terminal byte stream. The ID-only fallback must
// route the bytes to the migrated tab.
func TestRebindWorkspaceIDOrphanedOutputMsg(t *testing.T) {
	oldWS, newWS := centerRebindWorkspacePair(t)

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())

	tab := &Tab{
		ID:          TabID("tab-1"),
		Workspace:   oldWS,
		SessionName: "amux-orphaned-output-session",
		Running:     true,
		State:       ptyio.State{},
	}
	m.tabs.ByWorkspace[oldID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0

	m.RebindWorkspaceID(oldWS, newWS)

	_ = m.updatePTYOutput(PTYOutput{WorkspaceID: oldID, TabID: tab.ID, Data: []byte("orphaned-bytes")})
	if got := string(tab.PendingOutput); !strings.Contains(got, "orphaned-bytes") {
		t.Fatalf("orphaned PTYOutput was dropped — stale workspace ID did not fall back to the tab (PendingOutput=%q)", got)
	}
}
