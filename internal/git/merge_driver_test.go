package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWithConflictingAttributedFile builds main+feature with a conflicting
// edit on shared.txt and `shared.txt merge=<driver>` committed on main — the
// shape that forces a content-level merge through the named driver.
func repoWithConflictingAttributedFile(t *testing.T, driver string) string {
	t.Helper()
	root := initRepo(t)
	commitFile(t, root, "shared.txt", "base\n", "add shared")
	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "shared.txt", "feature version\n", "feature edit")
	runGit(t, root, "checkout", "main")
	commitFile(t, root, ".gitattributes", "shared.txt merge="+driver+"\n", "attribute shared.txt")
	commitFile(t, root, "shared.txt", "main version\n", "main edit")
	return root
}

// TestMergeWorkspaceBranchRefusesLiveMergeDriver is the exploit case: a repo
// carrying a merge.<driver>.driver command plus an attributed, conflicting
// file would execute that command during a routine merge. The merge must
// refuse before git runs — the probe file the driver writes proves it never
// executed.
func TestMergeWorkspaceBranchRefusesLiveMergeDriver(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithConflictingAttributedFile(t, "evil")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "merge.evil.driver", "touch "+probe)

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	if err == nil {
		t.Fatal("expected a refusal, got a clean merge")
	}
	if errors.Is(err, ErrMergeConflict) {
		t.Fatalf("refusal misreported as a conflict: %v", err)
	}
	if !strings.Contains(err.Error(), "evil") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
	if !strings.Contains(err.Error(), "shared.txt") {
		t.Fatalf("refusal does not name the attributed path: %v", err)
	}
	if _, statErr := os.Stat(probe); !os.IsNotExist(statErr) {
		t.Fatal("the merge driver EXECUTED — the probe file exists")
	}
	// Refusal happens before git merge — no merge in progress to abort.
	if _, statErr := os.Stat(filepath.Join(root, ".git", "MERGE_HEAD")); !os.IsNotExist(statErr) {
		t.Fatal("refusal left a merge in progress")
	}
}

// TestMergeWorkspaceBranchPermitsUnattributedDriverConfig proves the refusal
// is scoped to live bindings: a configured driver nothing attributes can
// never fire, so the merge proceeds normally.
func TestMergeWorkspaceBranchPermitsUnattributedDriverConfig(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "merge.evil.driver", "touch "+probe)

	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("unattributed driver config must not block the merge: %v", err)
	}
	if _, statErr := os.Stat(probe); !os.IsNotExist(statErr) {
		t.Fatal("driver executed despite no attribution")
	}
}

// TestMergeWorkspaceBranchPermitsUnconfiguredAttribution proves the other
// half of the pair-check: `merge=<name>` with no merge.<name>.driver config
// uses git's built-in semantics — a builtin driver (union) merges cleanly
// and a configured-looking name falls back to the default driver.
func TestMergeWorkspaceBranchPermitsUnconfiguredAttribution(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithConflictingAttributedFile(t, "union")

	// union is a builtin driver — no config, no exec — and it resolves the
	// conflicting line edits by keeping both sides.
	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("builtin union merge must proceed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "main version") || !strings.Contains(string(content), "feature version") {
		t.Fatalf("union merge content = %q, want both sides kept", content)
	}
}

// TestMergeWorkspaceBranchDriverConfigWithoutAttributionUsesDefaultDriver —
// `merge=evil` attributed but `merge.evil.driver` never configured: git runs
// its default text merge, which conflicts normally and never execs.
func TestMergeWorkspaceBranchDriverAttributionWithoutConfig(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithConflictingAttributedFile(t, "evil")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	// NOTE: no merge.evil.driver config — the name is inert.

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	if err == nil {
		t.Fatal("expected the normal conflict path")
	}
	if !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("unconfigured attribution produced %v, want ErrMergeConflict", err)
	}
	if _, statErr := os.Stat(probe); !os.IsNotExist(statErr) {
		t.Fatal("an unconfigured driver name executed something")
	}
	if err := AbortMerge(context.Background(), root); err != nil {
		t.Fatalf("AbortMerge: %v", err)
	}
}
