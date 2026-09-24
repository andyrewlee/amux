package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
)

// driftedWorkspace returns a workspace loaded from a real store whose
// persisted key (ID/MetadataID) differs from ComputedID — the state every
// pre-stable-ID record can reach after a path-existence flip. rootCreated
// controls whether the worktree dir exists at return time, which selects
// which form ComputedID reports.
func driftedWorkspace(t *testing.T, rootCreated bool) (*data.Workspace, *data.WorkspaceStore) {
	t.Helper()
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(filepath.Join(target, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ws := data.NewWorkspace("feat", "feat", "main",
		filepath.Join(target, "repo"), filepath.Join(link, "feat"))
	store := data.NewWorkspaceStore(filepath.Join(base, "store"))
	// Save before the root exists: the minted key is the unresolved form.
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	if rootCreated {
		if err := os.MkdirAll(filepath.Join(link, "feat"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if ws.ID() == ws.ComputedID() {
		t.Fatalf("fixture must produce divergent identity forms (id=%s)", ws.ID())
	}
	return ws, store
}

// TestFindWorkspaceByID_ResolvesLegacyComputedForm: a session tagged under
// the form ws.ID() computed at spawn (resolved) must still resolve to the
// workspace after the persisted key settled on the unresolved form.
func TestFindWorkspaceByID_ResolvesLegacyComputedForm(t *testing.T) {
	ws, _ := driftedWorkspace(t, true)
	app := lifecycleTestApp(t)
	project := data.NewProject(ws.Repo)
	project.Workspaces = []data.Workspace{*ws}
	app.projects = []data.Project{*project}

	if got := app.findWorkspaceByID(string(ws.ID())); got == nil {
		t.Fatal("stable ID must resolve")
	}
	legacyForm := string(ws.ComputedID())
	got := app.findWorkspaceByID(legacyForm)
	if got == nil {
		t.Fatalf("computed form %s must resolve via fallback", legacyForm)
	}
	if got.Name != ws.Name {
		t.Fatalf("resolved to %q, want %q", got.Name, ws.Name)
	}
}

// TestCollectKnownWorkspaceIDs_IncludesComputedForm: orphan GC treats
// unrecognized session tags as orphaned — every identity form a live
// workspace could have stamped must read as known.
func TestCollectKnownWorkspaceIDs_IncludesComputedForm(t *testing.T) {
	ws, _ := driftedWorkspace(t, true)
	app := lifecycleTestApp(t)
	project := data.NewProject(ws.Repo)
	project.Workspaces = []data.Workspace{*ws}
	app.projects = []data.Project{*project}

	ids := app.collectKnownWorkspaceIDs()
	for _, form := range []data.WorkspaceID{ws.ID(), ws.MetadataID(), ws.ComputedID()} {
		if !ids[string(form)] {
			t.Fatalf("known IDs missing form %s (have %v)", form, ids)
		}
	}
}

// formAwareTagOps answers SessionsWithTags with a row per queried
// @amux_workspace form, recording every match it saw.
type formAwareTagOps struct {
	tmuxops.FakeTmuxOps
	seen []string
}

func (f *formAwareTagOps) SessionsWithTags(match map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
	form := match["@amux_workspace"]
	f.seen = append(f.seen, form)
	return []tmux.SessionTagValues{{
		Name: "amux-" + form + "-sess",
		Tags: map[string]string{"@amux_workspace": form},
	}}, nil
}

// TestSessionsWithWorkspaceTag_MergesAllForms: discovery must query every
// identity form — pre-upgrade sessions can carry whichever form ws.ID()
// returned at their spawn time.
func TestSessionsWithWorkspaceTag_MergesAllForms(t *testing.T) {
	ws, _ := driftedWorkspace(t, true)
	forms := workspacesvc.WorkspaceIDStrings(ws)
	if len(forms) < 2 {
		t.Fatalf("fixture must yield multiple identity forms, got %v", forms)
	}
	ops := &formAwareTagOps{}
	rows, err := sessionsWithWorkspaceTag(ops, forms, "@amux_type", "agent", nil, tmux.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(forms) {
		t.Fatalf("rows = %v, want one per form %v", rows, forms)
	}
	if len(ops.seen) != len(forms) {
		t.Fatalf("queried forms = %v, want all of %v", ops.seen, forms)
	}
}
