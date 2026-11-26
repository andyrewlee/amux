package workspacesvc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// gitTestRepo creates a real git repo (git worktree list needs one) under dir.
// Every git call goes through testutil.RunGit, which strips the inherited
// GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE that git exports to hooks: the
// pre-push hook runs the test sweep with GIT_DIR set, and an inherited value
// hijacks every child command into the real repo whatever cmd.Dir says.
func gitTestRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	testutil.RunGit(t, dir, "init", "-b", "main")
	testutil.RunGit(t, dir, "config", "user.email", "amux@example.com")
	testutil.RunGit(t, dir, "config", "user.name", "amux")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.RunGit(t, dir, "add", "README.md")
	testutil.RunGit(t, dir, "commit", "-m", "init")
}

// TestLoadProjectsRescanDoesNotFlapArchiveDriftedKey pins the rescan archive
// check: a stored record whose storeID was minted while its root didn't
// resolve (drifted key) must still match the discovered worktree by
// canonical repo+root — never flap hidden↔visible across rescans.
func TestLoadProjectsRescanDoesNotFlapArchiveDriftedKey(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	repo := filepath.Join(realDir, "repo")
	managed := filepath.Join(realDir, "managed")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitTestRepo(t, repo)

	store := data.NewWorkspaceStore(filepath.Join(base, "metadata"))
	registry := &testutil.FakeProjectRegistry{ProjectsFunc: func() ([]string, error) { return []string{repo}, nil }}
	// Managed root passed in the LINK form: stored record Root keeps the
	// unresolved spelling while git discovers the resolved worktree path.
	svc := New(registry, store, nil, filepath.Join(link, "managed"))

	// Save the record BEFORE the worktree root exists — the store key is
	// minted under the unresolved link form.
	ws := data.NewWorkspace("feat", "feat", "main", repo, filepath.Join(link, "managed", "feat"))
	ws.Created = ws.Created.Add(-1)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	storeID := ws.ID()

	// Now the worktree appears (git reports the resolved path) — the record's
	// ComputedID flips to the resolved form while storeID keeps the old key.
	// RunGit, not a raw exec: inherited GIT_DIR would aim the worktree
	// registration at the real repo (see gitTestRepo).
	testutil.RunGit(t, repo, "worktree", "add", filepath.Join(realDir, "managed", "feat"), "-b", "feat")

	// Seed a genuinely-missing record that SHOULD still archive.
	gone := data.NewWorkspace("gone", "gone", "main", repo, filepath.Join(link, "managed", "gone"))
	if err := store.Save(gone); err != nil {
		t.Fatal(err)
	}
	goneID := gone.ID()

	// Sanity: the drifted record's storeID must differ from its live
	// ComputedID, or this test proves nothing.
	live := data.NewWorkspace("feat", "feat", "main", repo, filepath.Join(link, "managed", "feat"))
	if live.ComputedID() == storeID {
		t.Fatalf("fixture produced no drift: storeID=%s ComputedID=%s", storeID, live.ComputedID())
	}

	for i := 0; i < 2; i++ {
		_ = svc.RescanWorkspaces()()
		stored, err := store.Load(storeID)
		if err != nil {
			t.Fatalf("rescan %d: drifted record unreadable: %v", i, err)
		}
		if stored.Archived {
			t.Fatalf("rescan %d archived a live worktree (drifted-key flap)", i)
		}
		storedGone, err := store.Load(goneID)
		if err != nil {
			t.Fatalf("rescan %d: missing-root record unreadable: %v", i, err)
		}
		if !storedGone.Archived {
			t.Fatalf("rescan %d did not archive a genuinely missing worktree", i)
		}
	}
}

// TestLoadProjectsRescanSkipsTombstonedDiscovery pins the tombstone
// guard: a discovered worktree carrying a durable .deleting tombstone must
// not be re-imported — the delete flow still owns it (worktree present,
// recovery unfinished). Without the check, UpsertFromDiscovery resurrects
// the row mid-delete.
func TestLoadProjectsRescanSkipsTombstonedDiscovery(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	managed := filepath.Join(base, "managed")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestRepo(t, repo)

	store := data.NewWorkspaceStore(filepath.Join(base, "metadata"))
	registry := &testutil.FakeProjectRegistry{ProjectsFunc: func() ([]string, error) { return []string{repo}, nil }}
	svc := New(registry, store, nil, managed)

	wtPath := filepath.Join(managed, "feat")
	// RunGit, not a raw exec: inherited GIT_DIR would aim the worktree
	// registration at the real repo (see gitTestRepo).
	testutil.RunGit(t, repo, "worktree", "add", wtPath, "-b", "feat")

	// The discovered workspace computes its identity at discovery time —
	// which may resolve /var→/private/var on macOS. Mark the tombstone under
	// every repo×root form the discovery could compute so the guard fires
	// regardless of resolution.
	resolvedRepo, _ := filepath.EvalSymlinks(repo)
	resolvedWT, _ := filepath.EvalSymlinks(wtPath)
	for _, r := range []string{repo, resolvedRepo} {
		for _, p := range []string{wtPath, resolvedWT} {
			ws := data.NewWorkspace("feat", "feat", "main", r, p)
			if err := store.MarkDeleting(ws.ComputedID()); err != nil {
				t.Fatalf("MarkDeleting: %v", err)
			}
		}
	}

	// Positive control: a second untombstoned worktree must import in the
	// same rescan, proving discovery actually ran.
	cleanWT := filepath.Join(managed, "clean")
	testutil.RunGit(t, repo, "worktree", "add", cleanWT, "-b", "clean")

	_ = svc.RescanWorkspaces()()
	for _, r := range []string{repo, resolvedRepo} {
		for _, p := range []string{wtPath, resolvedWT} {
			ws := data.NewWorkspace("feat", "feat", "main", r, p)
			if _, err := store.Load(ws.ComputedID()); err == nil {
				t.Fatalf("rescan re-imported a tombstoned workspace mid-delete (repo=%s root=%s)", r, p)
			}
		}
	}
	resolvedClean, _ := filepath.EvalSymlinks(cleanWT)
	imported := false
	for _, r := range []string{repo, resolvedRepo} {
		for _, p := range []string{cleanWT, resolvedClean} {
			ws := data.NewWorkspace("clean", "clean", "main", r, p)
			if _, err := store.Load(ws.ComputedID()); err == nil {
				imported = true
			}
		}
	}
	if !imported {
		t.Fatal("control worktree was not imported — discovery did not run")
	}
}
