package data

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestRepoHintFromRawJSONHandlesEscapedQuotes(t *testing.T) {
	repo := `/tmp/path/with"quote/repo`
	content := `{"repo":` + strconv.Quote(repo) + `, bad`

	hint, ok := repoHintFromRawJSON([]byte(content))
	if !ok {
		t.Fatal("repoHintFromRawJSON() should recover repo hint from escaped quoted string")
	}
	if hint != repo {
		t.Fatalf("repoHintFromRawJSON() = %q, want %q", hint, repo)
	}
}

func TestWorkspaceStore_ListByRepoHandlesQuotedRepoHintFromCorruptMetadata(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	base := t.TempDir()
	targetRepo := filepath.Join(base, `target"repo`)
	otherRepo := filepath.Join(base, "other")
	otherRoot := filepath.Join(otherRepo, ".amux", "workspaces", "ws1")
	for _, dir := range []string{targetRepo, otherRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	corruptID := WorkspaceID("corrupt-quoted-repo-hint")
	corruptDir := filepath.Join(storeRoot, string(corruptID))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	corruptJSON := `{"repo":` + strconv.Quote(targetRepo) + `, bad`
	if err := os.WriteFile(filepath.Join(corruptDir, "workspace.json"), []byte(corruptJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	ws := &Workspace{
		Name: "ws1",
		Repo: otherRepo,
		Root: otherRoot,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save(otherRepo) error = %v", err)
	}

	if _, err := store.ListByRepo(targetRepo); err == nil {
		t.Fatal("ListByRepo(targetRepo) should return error for target-repo corrupt metadata")
	}

	workspaces, err := store.ListByRepo(otherRepo)
	if err != nil {
		t.Fatalf("ListByRepo(otherRepo) error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace for otherRepo, got %d", len(workspaces))
	}
}

func TestWorkspaceStore_ListByRepo_IgnoresUnrelatedCorruptMetadata(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	base := t.TempDir()
	corruptRepo := filepath.Join(base, "corrupt-repo")
	validRepo := filepath.Join(base, "valid-repo")
	validRoot := filepath.Join(validRepo, ".amux", "workspaces", "ws1")
	for _, dir := range []string{corruptRepo, validRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	corruptID := WorkspaceID("corrupt-entry-xyz")
	corruptDir := filepath.Join(storeRoot, string(corruptID))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	corruptJSON := `{"repo": "` + corruptRepo + `", bad json`
	if err := os.WriteFile(filepath.Join(corruptDir, "workspace.json"), []byte(corruptJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	ws := &Workspace{
		Name:   "ws1",
		Branch: "main",
		Repo:   validRepo,
		Root:   validRoot,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	workspaces, err := store.ListByRepo(validRepo)
	if err != nil {
		t.Fatalf("ListByRepo(validRepo) error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace for validRepo, got %d", len(workspaces))
	}

	if _, err := store.ListByRepo(corruptRepo); err == nil {
		t.Fatal("ListByRepo(corruptRepo) should return error for target-repo corruption")
	}
}

func TestWorkspaceStore_ListByRepo_SurfacesUnknownCorruptMetadata(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	base := t.TempDir()
	targetRepo := filepath.Join(base, "target-repo")
	otherRepo := filepath.Join(base, "other-repo")
	otherRoot := filepath.Join(otherRepo, ".amux", "workspaces", "ws1")
	for _, dir := range []string{targetRepo, otherRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	corruptID := WorkspaceID("corrupt-no-repo-hint")
	corruptDir := filepath.Join(storeRoot, string(corruptID))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "workspace.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	ws := &Workspace{
		Name:   "ws1",
		Branch: "main",
		Repo:   otherRepo,
		Root:   otherRoot,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := store.ListByRepo(targetRepo); err == nil {
		t.Fatal("ListByRepo(targetRepo) should return error when unknown-repo corruption exists and no results found")
	}

	workspaces, err := store.ListByRepo(otherRepo)
	if err != nil {
		t.Fatalf("ListByRepo(otherRepo) error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace for otherRepo, got %d", len(workspaces))
	}
}

func TestCanonicalLookupPath_KeepsRelativeSymlinkPathRelative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}
	base := t.TempDir()
	realRepo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRepo) error = %v", err)
	}
	if err := os.Symlink("real/repo", filepath.Join(base, "repo-link")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(base) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousWD) })

	got := canonicalLookupPath("./repo-link")
	want := "repo-link"
	if got != want {
		t.Fatalf("canonicalLookupPath(relative symlink) = %q, want %q", got, want)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("canonicalLookupPath(relative symlink) should stay relative, got %q", got)
	}
}

func TestCanonicalLookupPath_ResolvesAbsoluteSymlinkPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require elevated privileges on windows")
	}
	base := t.TempDir()
	realRepo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRepo) error = %v", err)
	}
	linkPath := filepath.Join(base, "repo-link")
	if err := os.Symlink(realRepo, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	got := canonicalLookupPath(linkPath)
	want := NormalizePath(realRepo)
	if got != want {
		t.Fatalf("canonicalLookupPath(absolute symlink) = %q, want %q", got, want)
	}
}
