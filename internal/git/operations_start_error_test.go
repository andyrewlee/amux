package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGitCtxStartErrorIncludesCause(t *testing.T) {
	skipIfNoGit(t)

	missingDir := filepath.Join(t.TempDir(), "missing")
	_, err := RunGitCtx(context.Background(), missingDir, "status")
	if err == nil {
		t.Fatal("expected invalid working directory error")
	}
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected structured git error, got %T", err)
	}
	if gitErr.Stderr != "" {
		t.Fatalf("expected start failure to have empty stderr, got %q", gitErr.Stderr)
	}
	if !strings.Contains(err.Error(), "chdir") && !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected error string to include start failure cause, got %v", err)
	}
}
