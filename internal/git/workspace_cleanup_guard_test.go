package git

import (
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
