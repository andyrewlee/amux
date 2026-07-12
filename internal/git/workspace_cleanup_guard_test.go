package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafeWorkspaceCleanupPath(t *testing.T) {
	home, homeErr := os.UserHomeDir()

	tests := []struct {
		name string
		path string
		want bool
		// needsHome rows depend on os.UserHomeDir(); they are skipped rather
		// than failed when a home directory cannot be resolved.
		needsHome bool
	}{
		{name: "empty", path: "", want: false},
		{name: "root", path: "/", want: false},
		{name: "dot", path: ".", want: false},
		{name: "root via clean", path: "/tmp/..", want: false},
		{name: "home", path: home, want: false, needsHome: true},
		{name: "home via clean", path: home + "/subdir/..", want: false, needsHome: true},
		{name: "home subdirectory", path: filepath.Join(home, ".amux", "workspaces", "repo", "feature"), want: true, needsHome: true},
		{name: "normal tmp path", path: filepath.Join(t.TempDir(), "workspace"), want: true},
		{name: "relative path", path: "some/relative/workspace", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needsHome && homeErr != nil {
				t.Skipf("os.UserHomeDir() unavailable: %v", homeErr)
			}
			if got := isSafeWorkspaceCleanupPath(tt.path); got != tt.want {
				t.Errorf("isSafeWorkspaceCleanupPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestHasManagedWorkspaceAncestorForRepo pins the fallback guard used by
// isLegacyManagedWorkspacePathForRepo when no managed-workspaces root aliases
// resolve. A false here must keep the destructive cleanup path disabled.
func TestHasManagedWorkspaceAncestorForRepo(t *testing.T) {
	base := t.TempDir()
	repoSegments := map[string]struct{}{"myrepo": {}}

	tests := []struct {
		name     string
		path     string
		segments map[string]struct{}
		want     bool
	}{
		{
			name:     "workspace under managed ancestor for repo",
			path:     filepath.Join(base, ".amux", "workspaces", "myrepo", "feature-branch"),
			segments: repoSegments,
			want:     true,
		},
		{
			name:     "nested path under managed ancestor for repo",
			path:     filepath.Join(base, ".amux", "workspaces", "myrepo", "feature", "sub", "dir"),
			segments: repoSegments,
			want:     true,
		},
		{
			name:     "workspace for a different repo segment",
			path:     filepath.Join(base, ".amux", "workspaces", "otherrepo", "feature-branch"),
			segments: repoSegments,
			want:     false,
		},
		{
			name:     "no .amux/workspaces ancestor",
			path:     filepath.Join(base, "projects", "myrepo", "feature-branch"),
			segments: repoSegments,
			want:     false,
		},
		{
			name:     "workspaces dir not under .amux",
			path:     filepath.Join(base, "stuff", "workspaces", "myrepo", "feature-branch"),
			segments: repoSegments,
			want:     false,
		},
		{
			name:     "repo dir itself, no workspace segment below",
			path:     filepath.Join(base, ".amux", "workspaces", "myrepo"),
			segments: repoSegments,
			want:     false,
		},
		{
			name:     "the workspaces root itself",
			path:     filepath.Join(base, ".amux", "workspaces"),
			segments: repoSegments,
			want:     false,
		},
		{
			name:     "empty repo segments",
			path:     filepath.Join(base, ".amux", "workspaces", "myrepo", "feature-branch"),
			segments: map[string]struct{}{},
			want:     false,
		},
		{
			name:     "relative dot path",
			path:     ".",
			segments: repoSegments,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasManagedWorkspaceAncestorForRepo(tt.path, tt.segments); got != tt.want {
				t.Errorf("hasManagedWorkspaceAncestorForRepo(%q, %v) = %v, want %v", tt.path, tt.segments, got, tt.want)
			}
		})
	}
}

// TestIsGitRepoUnavailableError pins the classifier that gates the rm -rf
// cleanup recovery when git reports the repo is unavailable. The matched
// condition is transcribed from the function: the lowercased error message
// contains "not a git repository". The nil case is intentionally absent:
// the function dereferences err unconditionally and its only call site
// (gitCommonDirWithContext) guards err != nil.
func TestIsGitRepoUnavailableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "git fatal not-a-repository message",
			err:  errors.New("fatal: not a git repository (or any of the parent directories): .git"),
			want: true,
		},
		{
			name: "mixed case is lowered before matching",
			err:  errors.New("fatal: Not a Git Repository"),
			want: true,
		},
		{
			name: "wrapped error keeps the matched substring",
			err:  fmt.Errorf("run git: %w", errors.New("not a git repository")),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("permission denied"),
			want: false,
		},
		{
			name: "empty message",
			err:  errors.New(""),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGitRepoUnavailableError(tt.err); got != tt.want {
				t.Errorf("isGitRepoUnavailableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
