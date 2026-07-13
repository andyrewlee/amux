package sidebar

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/testutil"
)

// newFeatureWorkspaceRepo builds a temp repo on "main" with one commit ahead
// on "feature" (checked out), and returns a *data.Workspace pointed at it —
// the fixture the branch-mode fetch commands (loadBranchChanges,
// refreshAheadBehind) exercise end-to-end against a real git repo.
func newFeatureWorkspaceRepo(t *testing.T) (*data.Workspace, string) {
	t.Helper()
	repo := testutil.InitRepo(t)
	testutil.RunGit(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(repo+"/widget.go", []byte("package widget\n"), 0o600); err != nil {
		t.Fatalf("write widget.go: %v", err)
	}
	testutil.RunGit(t, repo, "add", "widget.go")
	testutil.RunGit(t, repo, "commit", "-m", "add widget")
	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)
	return ws, repo
}

func TestToggleBranchModeFetchesAndPopulatesDisplayItems(t *testing.T) {
	ws, _ := newFeatureWorkspaceRepo(t)

	m := New()
	m.SetSize(60, 20)
	m.SetWorkspace(ws)

	cmd := m.toggleBranchMode()
	if !m.branchMode {
		t.Fatal("toggleBranchMode() should turn branch mode on")
	}
	if !m.branchLoading {
		t.Fatal("toggleBranchMode() should mark branchLoading while the fetch is in flight")
	}
	if cmd == nil {
		t.Fatal("toggleBranchMode() should return a fetch command when turning on")
	}

	msg := cmd()
	loaded, ok := msg.(BranchChangesLoaded)
	if !ok {
		t.Fatalf("fetch command produced %T, want BranchChangesLoaded", msg)
	}
	if loaded.Err != nil {
		t.Fatalf("BranchChangesLoaded.Err = %v, want nil", loaded.Err)
	}
	if len(loaded.Changes) != 1 || loaded.Changes[0].Path != "widget.go" {
		t.Fatalf("BranchChangesLoaded.Changes = %+v, want [widget.go]", loaded.Changes)
	}

	newModel, followUpCmd := m.Update(loaded)
	m = newModel
	if followUpCmd != nil {
		t.Errorf("Update(BranchChangesLoaded) returned a command, want nil")
	}
	if m.branchLoading {
		t.Error("branchLoading should be false once the result lands")
	}

	var fileItems int
	for _, item := range m.displayItems {
		if item.isHeader {
			continue
		}
		fileItems++
		if item.mode != git.DiffModeBranch {
			t.Errorf("displayItem.mode = %v, want DiffModeBranch", item.mode)
		}
		if item.change.Path != "widget.go" {
			t.Errorf("displayItem.change.Path = %q, want widget.go", item.change.Path)
		}
	}
	if fileItems != 1 {
		t.Fatalf("displayItems has %d file rows, want 1: %+v", fileItems, m.displayItems)
	}

	// Toggling off should fall back to the working-tree list without
	// disturbing the fetched branch data.
	if cmd := m.toggleBranchMode(); cmd != nil {
		t.Error("toggling branch mode off should not issue a fetch")
	}
	if m.branchMode {
		t.Error("branch mode should be off")
	}
}

func TestToggleBranchModeOffRestoresWorkingTreeList(t *testing.T) {
	m := New()
	m.SetSize(60, 20)
	m.SetWorkspace(data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo"))
	m.SetGitStatus(&git.StatusResult{
		Unstaged: []git.Change{{Path: "a.go", Kind: git.ChangeModified}},
	})

	// Force branch mode on synthetically (skip the real fetch) then back off.
	m.branchMode = true
	m.branchChanges = []git.Change{{Path: "b.go", Kind: git.ChangeAdded}}
	m.rebuildDisplayList()

	m.toggleBranchMode()
	if m.branchMode {
		t.Fatal("expected branch mode to toggle off")
	}

	var paths []string
	for _, item := range m.displayItems {
		if !item.isHeader {
			paths = append(paths, item.change.Path)
		}
	}
	if len(paths) != 1 || paths[0] != "a.go" {
		t.Fatalf("displayItems after toggling off = %v, want [a.go] (working tree)", paths)
	}
}

func TestRebuildBranchDisplayListRespectsFilter(t *testing.T) {
	m := New()
	m.branchMode = true
	// Pre-sorted, mirroring what BranchChangesVsBase returns in production
	// (rebuildBranchDisplayList itself doesn't sort — it trusts its input).
	m.branchChanges = []git.Change{
		{Path: "cmd/main.go", Kind: git.ChangeModified},
		{Path: "internal/bar.go", Kind: git.ChangeAdded},
		{Path: "internal/foo.go", Kind: git.ChangeModified},
	}
	m.filterQuery = "internal"

	m.rebuildDisplayList()

	if !m.displayItems[0].isHeader || m.displayItems[0].header != "Vs base (2)" {
		t.Fatalf("expected header 'Vs base (2)', got %+v", m.displayItems[0])
	}
	var paths []string
	for _, item := range m.displayItems {
		if !item.isHeader {
			paths = append(paths, item.change.Path)
		}
	}
	if len(paths) != 2 || paths[0] != "internal/bar.go" || paths[1] != "internal/foo.go" {
		t.Fatalf("filtered branch paths = %v, want [internal/bar.go internal/foo.go]", paths)
	}
}

func TestHandleBranchChangesLoadedDropsStaleResults(t *testing.T) {
	m := New()
	m.SetWorkspace(data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo"))
	m.branchMode = true
	m.branchLoading = true
	m.branchLoadID = 2 // a newer toggle already bumped this past the in-flight fetch's ID

	m.handleBranchChangesLoaded(BranchChangesLoaded{
		Root:    "/tmp/repo",
		LoadID:  1, // stale
		Changes: []git.Change{{Path: "stale.go", Kind: git.ChangeModified}},
	})
	if !m.branchLoading || m.branchChanges != nil {
		t.Fatalf("stale LoadID result should be dropped, got loading=%v changes=%+v", m.branchLoading, m.branchChanges)
	}

	m.handleBranchChangesLoaded(BranchChangesLoaded{
		Root:    "/some/other/repo",
		LoadID:  2,
		Changes: []git.Change{{Path: "other.go", Kind: git.ChangeModified}},
	})
	if !m.branchLoading || m.branchChanges != nil {
		t.Fatalf("result for a different workspace root should be dropped, got loading=%v changes=%+v", m.branchLoading, m.branchChanges)
	}

	m.handleBranchChangesLoaded(BranchChangesLoaded{
		Root:    "/tmp/repo",
		LoadID:  2,
		Changes: []git.Change{{Path: "current.go", Kind: git.ChangeModified}},
	})
	if m.branchLoading {
		t.Error("matching LoadID/Root should clear branchLoading")
	}
	if len(m.branchChanges) != 1 || m.branchChanges[0].Path != "current.go" {
		t.Fatalf("branchChanges = %+v, want [current.go]", m.branchChanges)
	}
}

func TestHandleAheadBehindLoadedUpdatesAndDropsStale(t *testing.T) {
	m := New()
	m.SetWorkspace(data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo"))
	m.aheadBehindLoadID = 3

	m.handleAheadBehindLoaded(AheadBehindLoaded{Root: "/tmp/repo", LoadID: 2, Ahead: 5, Behind: 5})
	if m.ahead != 0 || m.behind != 0 {
		t.Fatalf("stale LoadID should be dropped, got ahead=%d behind=%d", m.ahead, m.behind)
	}

	m.handleAheadBehindLoaded(AheadBehindLoaded{Root: "/tmp/repo", LoadID: 3, Ahead: 2, Behind: 1})
	if m.ahead != 2 || m.behind != 1 {
		t.Fatalf("ahead, behind = %d, %d, want 2, 1", m.ahead, m.behind)
	}

	wantErr := errors.New("boom")
	m.aheadBehindLoadID = 4
	m.handleAheadBehindLoaded(AheadBehindLoaded{Root: "/tmp/repo", LoadID: 4, Err: wantErr})
	if m.aheadBehindErr == nil {
		t.Fatal("expected aheadBehindErr to be set")
	}
	// An error result must not clobber the last-known good counts.
	if m.ahead != 2 || m.behind != 1 {
		t.Fatalf("ahead, behind after error = %d, %d, want unchanged 2, 1", m.ahead, m.behind)
	}
}

func TestSetWorkspaceResetsBranchStateAndFetchesAheadBehind(t *testing.T) {
	ws, repo := newFeatureWorkspaceRepo(t)

	m := New()
	m.SetWorkspace(data.NewWorkspace("other", "other", "main", "/tmp/other", "/tmp/other"))
	m.branchMode = true
	m.branchChanges = []git.Change{{Path: "leftover.go"}}
	m.ahead, m.behind = 9, 9

	cmd := m.SetWorkspace(ws)
	if m.branchMode {
		t.Error("SetWorkspace should turn branch mode off for the new workspace")
	}
	if m.branchChanges != nil {
		t.Error("SetWorkspace should clear stale branchChanges")
	}
	if m.ahead != 0 || m.behind != 0 {
		t.Errorf("SetWorkspace should zero ahead/behind pending the new fetch, got %d/%d", m.ahead, m.behind)
	}
	if cmd == nil {
		t.Fatal("SetWorkspace should return an ahead/behind fetch command for a genuinely new workspace")
	}

	msg := cmd()
	loaded, ok := msg.(AheadBehindLoaded)
	if !ok {
		t.Fatalf("fetch command produced %T, want AheadBehindLoaded", msg)
	}
	if loaded.Root != repo {
		t.Errorf("AheadBehindLoaded.Root = %q, want %q", loaded.Root, repo)
	}
	if loaded.Err != nil {
		t.Fatalf("AheadBehindLoaded.Err = %v, want nil", loaded.Err)
	}
	if loaded.Ahead != 1 || loaded.Behind != 0 {
		t.Errorf("ahead, behind = %d, %d, want 1, 0", loaded.Ahead, loaded.Behind)
	}
}

func TestSetWorkspaceNilOrRebindReturnsNilCmd(t *testing.T) {
	m := New()
	if cmd := m.SetWorkspace(nil); cmd != nil {
		t.Error("SetWorkspace(nil) should return a nil command")
	}

	ws := data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo")
	if cmd := m.SetWorkspace(ws); cmd == nil {
		t.Fatal("SetWorkspace with a new workspace should return a non-nil command")
	}
	// Re-setting the identical workspace (same canonical Root/Repo identity)
	// is a metadata rebind, not a switch, so it must not re-fetch.
	if cmd := m.SetWorkspace(ws); cmd != nil {
		t.Error("SetWorkspace with the identical *data.Workspace pointer should not re-fetch")
	}
}

func TestRenderAheadBehindBadge(t *testing.T) {
	m := New()
	m.SetWorkspace(data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo"))

	if got := m.renderAheadBehindBadge(); got != "" {
		t.Fatalf("badge with ahead=behind=0 = %q, want empty", got)
	}

	m.ahead, m.behind = 2, 0
	if got := m.renderAheadBehindBadge(); !strings.Contains(got, "2") {
		t.Fatalf("badge = %q, want it to mention ahead count 2", got)
	}

	m.aheadBehindErr = errors.New("boom")
	if got := m.renderAheadBehindBadge(); got != "" {
		t.Fatalf("badge after error = %q, want empty (hide rather than show stale/wrong data)", got)
	}
}

func TestRenderBranchSectionStates(t *testing.T) {
	m := New()
	m.SetSize(60, 20)
	m.SetWorkspace(data.NewWorkspace("ws", "ws", "main", "/tmp/repo", "/tmp/repo"))
	m.branchMode = true

	m.branchLoading = true
	if got := m.renderBranchSection(); !strings.Contains(got, "Loading") {
		t.Fatalf("loading state = %q, want it to mention Loading", got)
	}

	m.branchLoading = false
	m.branchErr = errors.New("git exploded")
	if got := m.renderBranchSection(); !strings.Contains(got, "git exploded") {
		t.Fatalf("error state = %q, want it to surface the error", got)
	}

	m.branchErr = nil
	m.branchChanges = nil
	if got := m.renderBranchSection(); !strings.Contains(got, "No commits ahead of base") {
		t.Fatalf("empty state = %q, want the no-commits message", got)
	}

	m.branchChanges = []git.Change{{Path: "widget.go", Kind: git.ChangeModified}}
	m.rebuildDisplayList()
	if got := m.renderBranchSection(); !strings.Contains(got, "widget.go") {
		t.Fatalf("populated state = %q, want it to list widget.go", got)
	}
}
