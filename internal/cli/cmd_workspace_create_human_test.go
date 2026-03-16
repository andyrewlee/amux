package cli

import (
	"errors"
	"testing"
)

func TestWorkspaceCreateSaveFailedHumanMessage(t *testing.T) {
	tests := []struct {
		name       string
		saveErr    error
		cleanupErr error
		want       string
	}{
		{
			name:    "save only",
			saveErr: errors.New("metadata write failed"),
			want:    "failed to save workspace metadata: metadata write failed",
		},
		{
			name:       "save and cleanup",
			saveErr:    errors.New("metadata write failed"),
			cleanupErr: errors.New("remove worktree: permission denied"),
			want:       "metadata write failed (cleanup failed: remove worktree: permission denied)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceCreateSaveFailedHumanMessage(tt.saveErr, tt.cleanupErr); got != tt.want {
				t.Fatalf("workspaceCreateSaveFailedHumanMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
