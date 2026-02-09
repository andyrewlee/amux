package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath_KeepsRelativePathStable(t *testing.T) {
	base := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(%s) error = %v", base, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	if got := NormalizePath("./repo/../repo"); got != "repo" {
		t.Fatalf("NormalizePath(relative) = %q, want %q", got, "repo")
	}
}

func TestWorkspaceID_RelativePathsIndependentOfCWD(t *testing.T) {
	base := t.TempDir()
	firstWD := filepath.Join(base, "first")
	secondWD := filepath.Join(base, "second")
	if err := os.MkdirAll(firstWD, 0o755); err != nil {
		t.Fatalf("MkdirAll(firstWD) error = %v", err)
	}
	if err := os.MkdirAll(secondWD, 0o755); err != nil {
		t.Fatalf("MkdirAll(secondWD) error = %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	ws := Workspace{
		Repo: "./repo",
		Root: "./repo/.amux/workspaces/feature",
	}

	if err := os.Chdir(firstWD); err != nil {
		t.Fatalf("Chdir(firstWD) error = %v", err)
	}
	idFromFirstWD := ws.ID()

	if err := os.Chdir(secondWD); err != nil {
		t.Fatalf("Chdir(secondWD) error = %v", err)
	}
	idFromSecondWD := ws.ID()

	if idFromFirstWD != idFromSecondWD {
		t.Fatalf("workspace IDs should be stable across CWD changes: %q vs %q", idFromFirstWD, idFromSecondWD)
	}
}
