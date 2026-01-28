package sandbox

import (
	"testing"
)

func TestComputeWorktreeID(t *testing.T) {
	tests := []struct {
		name     string
		cwd      string
		wantLen  int
		wantSame bool // if true, compare with previous test
	}{
		{
			name:    "absolute path",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
		{
			name:    "different path gives different ID",
			cwd:     "/home/user/other-project",
			wantLen: 16,
		},
		{
			name:    "same path gives same ID",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
	}

	var prevID string
	var firstID string

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeWorktreeID(tt.cwd)

			if len(got) != tt.wantLen {
				t.Errorf("ComputeWorktreeID() length = %d, want %d", len(got), tt.wantLen)
			}

			// Verify it's a valid hex string
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("ComputeWorktreeID() contains invalid hex char: %c", c)
				}
			}

			// Track IDs for comparison
			if i == 0 {
				firstID = got
			} else if i == 1 {
				// Second test should be different from first
				if got == firstID {
					t.Error("Different paths should produce different IDs")
				}
				prevID = got
			} else if i == 2 {
				// Third test (same path as first) should match first
				if got != firstID {
					t.Error("Same path should produce same ID")
				}
				// And be different from second
				if got == prevID {
					t.Error("Same path should not match different path's ID")
				}
			}
		})
	}
}

func TestComputeWorktreeID_Deterministic(t *testing.T) {
	cwd := "/home/user/my-project"

	// Call multiple times, should always return same value
	id1 := ComputeWorktreeID(cwd)
	id2 := ComputeWorktreeID(cwd)
	id3 := ComputeWorktreeID(cwd)

	if id1 != id2 || id2 != id3 {
		t.Errorf("ComputeWorktreeID should be deterministic: got %s, %s, %s", id1, id2, id3)
	}
}
