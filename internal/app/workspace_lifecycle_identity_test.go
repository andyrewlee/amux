package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestRunUnlessMutatingWorkspaceIDsRootBridge covers the rescan race where
// a mutation marked under one ID form must still block a callback
// for a workspace whose current ComputedID/ID differ (the worktree dir
// vanished or appeared mid-mutation, flipping path-dependent forms). The
// root bridge is what connects the marked key to the drifted value.
func TestRunUnlessMutatingWorkspaceIDsRootBridge(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/managed/feat"
	if !st.markMutatingWorkspace("drifted-mark-id", root, true) {
		t.Fatal("failed to mark mutation under drifted id")
	}
	ws := data.NewWorkspace("feat", "feat", "main", "/repo", root)
	if string(ws.ID()) == "drifted-mark-id" {
		t.Fatal("fixture produced no drift: ws.ID equals the marked key")
	}
	if st.runUnlessMutatingWorkspaceIDs(ws, func() {
		t.Fatal("callback ran while the workspace was mutation-in-flight")
	}) {
		t.Fatal("runUnlessMutatingWorkspaceIDs should report the callback skipped")
	}
	if !st.isMutatingWorkspaceIDs(ws) {
		t.Fatal("isMutatingWorkspaceIDs missed the root-bridged mark")
	}
}

// TestRunUnlessMutatingWorkspaceIDsRunsWhenClear is the sanity half: a
// workspace sharing neither identity form nor root with the mark must run.
func TestRunUnlessMutatingWorkspaceIDsRunsWhenClear(t *testing.T) {
	st := newWorkspaceLifecycleState()
	if !st.markMutatingWorkspace("marked-id", "/repo/managed/feat", true) {
		t.Fatal("failed to mark mutation")
	}
	other := data.NewWorkspace("other", "feat", "main", "/repo", "/repo/managed/other")
	ran := false
	if !st.runUnlessMutatingWorkspaceIDs(other, func() { ran = true }) {
		t.Fatal("guard skipped an unrelated workspace")
	}
	if !ran {
		t.Fatal("callback did not run")
	}
}

// TestClearCreatingWorkspaceDriftedMark covers the create mark/clear
// asymmetry: creating is marked under pending.ID() computed before the
// worktree dir exists, but the completion message carries the post-create
// workspace whose ID() may differ. clearCreatingWorkspace must release the
// marked key via the root bridge and identity set.
func TestClearCreatingWorkspaceDriftedMark(t *testing.T) {
	st := newWorkspaceLifecycleState()
	root := "/repo/managed/feat"
	if !st.markCreatingWorkspace("pre-create-id", root) {
		t.Fatal("failed to mark creating")
	}
	ws := data.NewWorkspace("feat", "feat", "main", "/repo", root)
	if string(ws.ID()) == "pre-create-id" {
		t.Fatal("fixture produced no drift")
	}
	st.clearCreatingWorkspace(ws)
	if st.phase("pre-create-id") == lifecycleCreating {
		t.Fatal("creating phase leaked under the pre-create id")
	}
	if id := st.creatingRootID[root]; id != "" {
		t.Fatalf("creatingRootID bridge entry leaked: %s", id)
	}
	// A later mutation mark under the post-drift identity must be accepted —
	// proves the phase is fully settled.
	if !st.markMutatingWorkspace(string(ws.ID()), root, true) {
		t.Fatal("post-clear mutation mark rejected")
	}
}

// TestClearCreatingWorkspaceIdentitySet verifies clearing also sweeps every
// identity form present in phases, not just the bridged mark.
func TestClearCreatingWorkspaceIdentitySet(t *testing.T) {
	st := newWorkspaceLifecycleState()
	ws := data.NewWorkspace("feat", "feat", "main", "/repo", "/repo/managed/feat")
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if !st.markCreating(id) {
			t.Fatalf("failed to mark creating under %s", id)
		}
	}
	st.clearCreatingWorkspace(ws)
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if st.phase(id) == lifecycleCreating {
			t.Fatalf("creating phase leaked under identity %s", id)
		}
	}
}

// TestMarkMutatingWorkspaceIDsRejectsDriftedOverlap covers the restore race:
// the worktree dir appearing mid-mutation flips NormalizePath's symlink
// resolution, so a request snapshot taken after the dir exists carries a
// ComputedID the original mark never stamped. The mark must reject on ANY
// in-flight identity form (here the stable storeID), not accept the fresh
// form — otherwise two worktree mutations run concurrently.
func TestMarkMutatingWorkspaceIDsRejectsDriftedOverlap(t *testing.T) {
	st := newWorkspaceLifecycleState()
	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := data.NewWorkspaceStore(t.TempDir())
	ws := data.NewWorkspace("feat", "feat", "main", realDir, filepath.Join(link, "feat"))
	if err := store.Save(ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	preDrift := ws.ComputedID()
	if !st.markMutatingWorkspaceIDs(ws, true) {
		t.Fatal("initial mark rejected")
	}
	// The leaf appearing resolves the symlinked parent — ComputedID changes.
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir worktree root: %v", err)
	}
	postDrift := ws.ComputedID()
	if postDrift == preDrift {
		t.Skip("workspace root does not traverse a symlink — no drift to exercise")
	}
	if st.markMutatingWorkspaceIDs(ws, true) {
		t.Fatal("mark under a partially drifted identity set was accepted")
	}
	// The in-flight op still owns the workspace: unmarking through the
	// drifted set releases every stamped form plus the root bridge.
	if !st.markMutatingWorkspaceIDs(ws, false) {
		t.Fatal("unmark rejected")
	}
	if st.isMutatingWorkspaceIDs(ws) {
		t.Fatal("unmark left a residue phase")
	}
	if !st.markMutatingWorkspaceIDs(ws, true) {
		t.Fatal("post-release mark rejected — phase leaked")
	}
}

// TestClearCreatingWorkspaceDoesNotStompMutation ensures a ws that moved on
// to mutating (e.g. create-failed cleanup racing) is not stomped back to
// active by a late create-completion clear.
func TestClearCreatingWorkspaceDoesNotStompMutation(t *testing.T) {
	st := newWorkspaceLifecycleState()
	ws := data.NewWorkspace("feat", "feat", "main", "/repo", "/repo/managed/feat")
	if !st.markMutatingWorkspace(string(ws.ID()), ws.Root, true) {
		t.Fatal("failed to mark mutation")
	}
	st.clearCreatingWorkspace(ws)
	if !st.isMutatingWorkspaceIDs(ws) {
		t.Fatal("clearCreatingWorkspace stomped a mutating phase")
	}
}
