package git

import (
	"context"
	"errors"
	"testing"
)

// TestParseNameStatusZ covers the NUL-delimited `git diff --name-status -z`
// record layout: STATUS\0path\0 for single-path statuses and
// R<score>\0old\0new\0 for renames/copies. Paths arrive as raw bytes — the
// whole point of -z is that tabs, newlines, and non-ASCII names are never
// C-quoted.
func TestParseNameStatusZ(t *testing.T) {
	t.Run("parses single-path and rename records with raw bytes", func(t *testing.T) {
		out := []byte("M\x00src/caf\xc3\xa9.go\x00" +
			"A\x00dir/with space.go\x00" +
			"R100\x00old\tname.go\x00new\tname.go\x00" +
			"D\x00gone.go\x00")

		changes := parseNameStatus(out)
		if len(changes) != 4 {
			t.Fatalf("parseNameStatus() returned %d changes, want 4: %+v", len(changes), changes)
		}
		byPath := make(map[string]Change, len(changes))
		for _, c := range changes {
			byPath[c.Path] = c
		}

		if c, ok := byPath["src/caf\u00e9.go"]; !ok || c.Kind != ChangeModified {
			t.Errorf("caf\u00e9 entry = %+v, want ChangeModified with raw UTF-8 path", c)
		}
		if c, ok := byPath["dir/with space.go"]; !ok || c.Kind != ChangeAdded {
			t.Errorf("space-in-path entry = %+v, want ChangeAdded", c)
		}
		if c, ok := byPath["new\tname.go"]; !ok || c.Kind != ChangeRenamed || c.OldPath != "old\tname.go" {
			t.Errorf("rename entry = %+v, want ChangeRenamed old=%q", c, "old\tname.go")
		}
		if c, ok := byPath["gone.go"]; !ok || c.Kind != ChangeDeleted {
			t.Errorf("delete entry = %+v, want ChangeDeleted", c)
		}
	})

	t.Run("copy record keeps both paths", func(t *testing.T) {
		out := []byte("C87\x00emoji \xf0\x9f\x8e\x89.go\x00emoji \xf0\x9f\x8e\x89 copy.go\x00")
		changes := parseNameStatus(out)
		if len(changes) != 1 {
			t.Fatalf("parseNameStatus() returned %d changes, want 1: %+v", len(changes), changes)
		}
		c := changes[0]
		if c.Kind != ChangeCopied || c.OldPath != "emoji \U0001F389.go" || c.Path != "emoji \U0001F389 copy.go" {
			t.Errorf("copy entry = %+v, want ChangeCopied with raw UTF-8 paths", c)
		}
	})

	t.Run("empty and truncated input", func(t *testing.T) {
		if got := parseNameStatus(nil); got != nil {
			t.Errorf("parseNameStatus(nil) = %+v, want nil", got)
		}
		if got := parseNameStatus([]byte("")); got != nil {
			t.Errorf("parseNameStatus(empty) = %+v, want nil", got)
		}
		// A rename record missing its second path must not consume garbage.
		got := parseNameStatus([]byte("M\x00ok.go\x00R100\x00orphan-old.go"))
		if len(got) != 1 || got[0].Path != "ok.go" {
			t.Errorf("truncated rename: got %+v, want only the complete M record", got)
		}
	})

	t.Run("results sorted by path", func(t *testing.T) {
		out := []byte("M\x00z.go\x00M\x00a.go\x00")
		changes := parseNameStatus(out)
		if len(changes) != 2 || changes[0].Path != "a.go" || changes[1].Path != "z.go" {
			t.Errorf("parseNameStatus() = %+v, want sorted [a.go z.go]", changes)
		}
	})
}

// TestBranchChangesVsBaseNonASCIIPaths is the integration half of the -z fix:
// a real repo whose paths need C-quoting under plain --name-status must come
// back unmangled.
func TestBranchChangesVsBaseNonASCIIPaths(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	writeFile(t, repo, "caf\u00e9 file.go", "package main\n")
	runGit(t, repo, "add", "caf\u00e9 file.go")
	runGit(t, repo, "commit", "-m", "seed unicode file")

	runGit(t, repo, "checkout", "-b", "feature")
	runGit(t, repo, "mv", "caf\u00e9 file.go", "caf\u00e9 renamed.go")
	writeFile(t, repo, "new\tfile.go", "package main\n")
	runGit(t, repo, "add", "new\tfile.go")
	runGit(t, repo, "commit", "-m", "rename and add odd paths")

	changes, err := BranchChangesVsBase(repo)
	if err != nil {
		t.Fatalf("BranchChangesVsBase() unexpected error: %v", err)
	}
	byPath := make(map[string]Change, len(changes))
	for _, c := range changes {
		byPath[c.Path] = c
	}
	if c, ok := byPath["caf\u00e9 renamed.go"]; !ok || c.Kind != ChangeRenamed || c.OldPath != "caf\u00e9 file.go" {
		t.Errorf("unicode rename = %+v, want ChangeRenamed from %q", c, "caf\u00e9 file.go")
	}
	if c, ok := byPath["new\tfile.go"]; !ok || c.Kind != ChangeAdded {
		t.Errorf("tab-in-path add = %+v, want ChangeAdded", c)
	}
}

// TestMergeWorkspaceBranchConflictReportsNonASCIIPath pins the merge.go half:
// a conflicted path that plain --name-only would C-quote must surface as the
// real filename.
func TestMergeWorkspaceBranchConflictReportsNonASCIIPath(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	commitFile(t, root, "caf\u00e9 shared.txt", "base\n", "add shared")

	runGit(t, root, "checkout", "-b", "feature")
	commitFile(t, root, "caf\u00e9 shared.txt", "feature version\n", "feature edit")

	runGit(t, root, "checkout", "main")
	commitFile(t, root, "caf\u00e9 shared.txt", "main version\n", "main edit")

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	var conflict *MergeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error %v does not carry a *MergeConflictError", err)
	}
	if len(conflict.Files) != 1 || conflict.Files[0] != "caf\u00e9 shared.txt" {
		t.Fatalf("conflicted files = %v, want [caf\u00e9 shared.txt] (raw, unquoted)", conflict.Files)
	}
}
