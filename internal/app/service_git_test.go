package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

// These tests exercise gitStatusService in service_git.go. The cache-facing
// methods (newGitStatusService, GetCached, UpdateCache, Invalidate) are pure
// logic over an in-memory git.StatusManager and are asserted directly, including
// the nil-receiver / nil-manager guard paths that protect the App during early
// init. Refresh and RefreshFast shell out to the git CLI, so they are driven
// against real temporary repositories (gated by skipIfNoGit) and assert the
// parsed StatusResult plus the line-stats contract that distinguishes the two.
//
// Skipped: the SPEC also listed a "Run" method, but gitStatusService has no such
// method in the current source, so there is nothing to test there.

// newGitStatusService must wrap the manager it is handed and never return nil.
func TestNewGitStatusService(t *testing.T) {
	t.Run("with manager", func(t *testing.T) {
		mgr := git.NewStatusManager()
		svc := newGitStatusService(mgr)
		if svc == nil {
			t.Fatal("newGitStatusService returned nil")
		}
		if svc.manager != mgr {
			t.Fatalf("newGitStatusService stored manager %p, want %p", svc.manager, mgr)
		}
	})

	t.Run("with nil manager", func(t *testing.T) {
		// A nil manager is tolerated at construction; the guarded methods turn it
		// into no-ops rather than panicking.
		svc := newGitStatusService(nil)
		if svc == nil {
			t.Fatal("newGitStatusService(nil) returned nil service")
		}
		if svc.manager != nil {
			t.Fatalf("newGitStatusService(nil) stored %p, want nil", svc.manager)
		}
	})
}

// GetCached round-trips through the underlying manager and honors the guard
// clauses for nil receiver / nil manager.
func TestGitStatusService_GetCached(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var svc *gitStatusService
		if got := svc.GetCached("/any"); got != nil {
			t.Fatalf("GetCached on nil receiver = %v, want nil", got)
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		svc := &gitStatusService{manager: nil}
		if got := svc.GetCached("/any"); got != nil {
			t.Fatalf("GetCached with nil manager = %v, want nil", got)
		}
	})

	t.Run("miss returns nil", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		if got := svc.GetCached("/never/cached"); got != nil {
			t.Fatalf("GetCached miss = %v, want nil", got)
		}
	})

	t.Run("hit returns cached value", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		want := &git.StatusResult{Clean: true}
		svc.UpdateCache("/root", want)
		got := svc.GetCached("/root")
		if got != want {
			t.Fatalf("GetCached hit = %p, want the cached pointer %p", got, want)
		}
	})

	t.Run("empty root", func(t *testing.T) {
		// An empty key is just another map key; a fresh manager has no entry for it.
		svc := newGitStatusService(git.NewStatusManager())
		if got := svc.GetCached(""); got != nil {
			t.Fatalf("GetCached(\"\") = %v, want nil before caching", got)
		}
		want := &git.StatusResult{Clean: false}
		svc.UpdateCache("", want)
		if got := svc.GetCached(""); got != want {
			t.Fatalf("GetCached(\"\") after caching = %p, want %p", got, want)
		}
	})
}

// UpdateCache stores a result so a subsequent GetCached returns it, overwrites
// prior entries, and is a no-op under the guard clauses.
func TestGitStatusService_UpdateCache(t *testing.T) {
	t.Run("nil receiver does not panic", func(t *testing.T) {
		var svc *gitStatusService
		svc.UpdateCache("/root", &git.StatusResult{}) // must not panic
	})

	t.Run("nil manager does not panic", func(t *testing.T) {
		svc := &gitStatusService{manager: nil}
		svc.UpdateCache("/root", &git.StatusResult{}) // must not panic
	})

	t.Run("stores then overwrites", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		first := &git.StatusResult{Clean: true}
		svc.UpdateCache("/root", first)
		if got := svc.GetCached("/root"); got != first {
			t.Fatalf("after first UpdateCache, GetCached = %p, want %p", got, first)
		}

		second := &git.StatusResult{Clean: false}
		svc.UpdateCache("/root", second)
		if got := svc.GetCached("/root"); got != second {
			t.Fatalf("after overwrite, GetCached = %p, want %p", got, second)
		}
	})

	t.Run("nil status value is cached as nil", func(t *testing.T) {
		// UpdateCache happily caches a nil *StatusResult; GetCached then returns
		// nil (a present-but-nil entry is indistinguishable from a miss here).
		svc := newGitStatusService(git.NewStatusManager())
		svc.UpdateCache("/root", nil)
		if got := svc.GetCached("/root"); got != nil {
			t.Fatalf("GetCached after caching nil = %v, want nil", got)
		}
	})

	t.Run("distinct roots are isolated", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		a := &git.StatusResult{Clean: true}
		b := &git.StatusResult{Clean: false}
		svc.UpdateCache("/a", a)
		svc.UpdateCache("/b", b)
		if got := svc.GetCached("/a"); got != a {
			t.Fatalf("GetCached(/a) = %p, want %p", got, a)
		}
		if got := svc.GetCached("/b"); got != b {
			t.Fatalf("GetCached(/b) = %p, want %p", got, b)
		}
	})
}

// Invalidate drops a single cached root and is a guarded no-op otherwise.
func TestGitStatusService_Invalidate(t *testing.T) {
	t.Run("nil receiver does not panic", func(t *testing.T) {
		var svc *gitStatusService
		svc.Invalidate("/root") // must not panic
	})

	t.Run("nil manager does not panic", func(t *testing.T) {
		svc := &gitStatusService{manager: nil}
		svc.Invalidate("/root") // must not panic
	})

	t.Run("removes cached entry", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		svc.UpdateCache("/root", &git.StatusResult{Clean: true})
		if got := svc.GetCached("/root"); got == nil {
			t.Fatal("precondition: expected entry to be cached")
		}
		svc.Invalidate("/root")
		if got := svc.GetCached("/root"); got != nil {
			t.Fatalf("GetCached after Invalidate = %v, want nil", got)
		}
	})

	t.Run("only the named root is removed", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		keep := &git.StatusResult{Clean: true}
		svc.UpdateCache("/keep", keep)
		svc.UpdateCache("/drop", &git.StatusResult{Clean: false})

		svc.Invalidate("/drop")
		if got := svc.GetCached("/drop"); got != nil {
			t.Fatalf("GetCached(/drop) after Invalidate = %v, want nil", got)
		}
		if got := svc.GetCached("/keep"); got != keep {
			t.Fatalf("GetCached(/keep) = %p, want %p (untouched)", got, keep)
		}
	})

	t.Run("unknown root is a no-op", func(t *testing.T) {
		svc := newGitStatusService(git.NewStatusManager())
		svc.Invalidate("/never/cached") // must not panic and must not error
	})
}

// setupDirtyRepo creates a real git repository containing one staged, one
// unstaged, and one untracked change, and returns its root.
func setupDirtyRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write tracked.txt: %v", err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "init")

	// Stage a brand new file.
	if err := os.WriteFile(filepath.Join(root, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write staged.txt: %v", err)
	}
	runGit(t, root, "add", "staged.txt")

	// Modify a tracked file without staging it.
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatalf("modify tracked.txt: %v", err)
	}

	// Leave an untracked file.
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("u1\nu2\nu3\n"), 0o644); err != nil {
		t.Fatalf("write untracked.txt: %v", err)
	}
	return root
}

func hasChange(changes []git.Change, path string) bool {
	for _, c := range changes {
		if c.Path == path {
			return true
		}
	}
	return false
}

// Refresh shells out to git and returns a parsed, line-stat-populated status.
func TestGitStatusService_Refresh(t *testing.T) {
	skipIfNoGit(t)
	svc := newGitStatusService(git.NewStatusManager())

	t.Run("clean repo", func(t *testing.T) {
		root := t.TempDir()
		runGit(t, root, "init", "-b", "main")
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ok\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
		runGit(t, root, "add", "README.md")
		runGit(t, root, "commit", "-m", "init")

		res, err := svc.Refresh(root)
		if err != nil {
			t.Fatalf("Refresh(clean): %v", err)
		}
		if res == nil {
			t.Fatal("Refresh(clean) = nil result")
		}
		if !res.Clean {
			t.Fatalf("Refresh(clean).Clean = false, want true (status %+v)", res)
		}
		if !res.HasLineStats {
			t.Fatal("Refresh must set HasLineStats=true")
		}
		if res.GetDirtyCount() != 0 {
			t.Fatalf("Refresh(clean) dirty count = %d, want 0", res.GetDirtyCount())
		}
	})

	t.Run("dirty repo groups changes and counts lines", func(t *testing.T) {
		root := setupDirtyRepo(t)

		res, err := svc.Refresh(root)
		if err != nil {
			t.Fatalf("Refresh(dirty): %v", err)
		}
		if res.Clean {
			t.Fatalf("Refresh(dirty).Clean = true, want false (status %+v)", res)
		}
		if !res.HasLineStats {
			t.Fatal("Refresh must set HasLineStats=true")
		}
		if !hasChange(res.Staged, "staged.txt") {
			t.Errorf("Refresh staged = %+v, want it to contain staged.txt", res.Staged)
		}
		if !hasChange(res.Unstaged, "tracked.txt") {
			t.Errorf("Refresh unstaged = %+v, want it to contain tracked.txt", res.Unstaged)
		}
		if !hasChange(res.Untracked, "untracked.txt") {
			t.Errorf("Refresh untracked = %+v, want it to contain untracked.txt", res.Untracked)
		}
		// staged.txt(+1), tracked.txt(+1), untracked.txt(+3) all add lines, so the
		// numstat-derived aggregate must be strictly positive (HasLineStats path).
		if res.TotalAdded <= 0 {
			t.Fatalf("Refresh TotalAdded = %d, want > 0", res.TotalAdded)
		}
	})

	t.Run("error on non-repo directory", func(t *testing.T) {
		// A plain temp dir is not a git work tree, so git status exits non-zero
		// and Refresh must surface the error rather than a result.
		res, err := svc.Refresh(t.TempDir())
		if err == nil {
			t.Fatalf("Refresh(non-repo) error = nil, want non-nil (got %+v)", res)
		}
	})
}

// RefreshFast returns the same grouping as Refresh but deliberately skips the
// expensive line-stat pass.
func TestGitStatusService_RefreshFast(t *testing.T) {
	skipIfNoGit(t)
	svc := newGitStatusService(git.NewStatusManager())

	t.Run("clean repo", func(t *testing.T) {
		root := t.TempDir()
		runGit(t, root, "init", "-b", "main")
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ok\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
		runGit(t, root, "add", "README.md")
		runGit(t, root, "commit", "-m", "init")

		res, err := svc.RefreshFast(root)
		if err != nil {
			t.Fatalf("RefreshFast(clean): %v", err)
		}
		if !res.Clean {
			t.Fatalf("RefreshFast(clean).Clean = false, want true (status %+v)", res)
		}
	})

	t.Run("dirty repo groups changes but skips line stats", func(t *testing.T) {
		root := setupDirtyRepo(t)

		res, err := svc.RefreshFast(root)
		if err != nil {
			t.Fatalf("RefreshFast(dirty): %v", err)
		}
		if res.Clean {
			t.Fatalf("RefreshFast(dirty).Clean = true, want false (status %+v)", res)
		}
		if !hasChange(res.Staged, "staged.txt") {
			t.Errorf("RefreshFast staged = %+v, want staged.txt", res.Staged)
		}
		if !hasChange(res.Unstaged, "tracked.txt") {
			t.Errorf("RefreshFast unstaged = %+v, want tracked.txt", res.Unstaged)
		}
		if !hasChange(res.Untracked, "untracked.txt") {
			t.Errorf("RefreshFast untracked = %+v, want untracked.txt", res.Untracked)
		}
		// The fast path is documented to skip numstat: HasLineStats stays false and
		// the aggregate totals stay zero even though there are real line additions.
		if res.HasLineStats {
			t.Error("RefreshFast must leave HasLineStats=false")
		}
		if res.TotalAdded != 0 || res.TotalDeleted != 0 {
			t.Fatalf("RefreshFast totals = (+%d, -%d), want (0, 0)", res.TotalAdded, res.TotalDeleted)
		}
	})

	t.Run("error on non-repo directory", func(t *testing.T) {
		res, err := svc.RefreshFast(t.TempDir())
		if err == nil {
			t.Fatalf("RefreshFast(non-repo) error = nil, want non-nil (got %+v)", res)
		}
	})
}
